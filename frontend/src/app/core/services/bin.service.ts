import { Injectable, inject, signal } from '@angular/core';
import type { HttpBinnedRequestSummary, HttpRequest } from '../models/http';
import { CommandApiService } from './command.service';
import { ChangeBus } from './change-bus.service';
import { RequestApiService } from './request.service';
import { CollectionApiService } from './collection.service';

export type { HttpBinnedRequestSummary };

/** One trash row to restore, and where to restore it to. */
export interface RestoreInput {
  bin_id: number;
  collection_id: number;
}

/**
 * Angular wrapper around the trash-bin commands: list, restore (undoable),
 * and destroy for good (forgets the undo steps that involved the rows).
 */
@Injectable({ providedIn: 'root' })
export class BinApiService {
  private readonly commands = inject(CommandApiService);
  private readonly requestApi = inject(RequestApiService);
  private readonly collectionApi = inject(CollectionApiService);
  private readonly bus = inject(ChangeBus);
  private projectId: number | null = null;

  readonly binnedRequests = signal<HttpBinnedRequestSummary[]>([]);

  constructor() {
    this.bus.changesFor('bin').subscribe(() => {
      if (this.projectId) void this.loadForProject(this.projectId);
    });
  }

  async loadForProject(projectId: number): Promise<void> {
    this.projectId = projectId;
    const all = await this.commands.execute<HttpBinnedRequestSummary[]>(
      'bin.list',
      { project_id: projectId },
    );
    this.binnedRequests.set(all ?? []);
  }

  /** A binned request with what it sends; the id is the bin id. */
  get(binId: number): Promise<HttpRequest> {
    return this.commands.execute<HttpRequest>('bin.get', { id: binId });
  }

  clear(): void {
    this.binnedRequests.set([]);
  }

  async restore(binId: number, targetCollectionId: number): Promise<HttpRequest> {
    const restored = await this.restoreMany([{ bin_id: binId, collection_id: targetCollectionId }]);
    return restored[0];
  }

  async restoreMany(inputs: RestoreInput[]): Promise<HttpRequest[]> {
    if (inputs.length === 0) return [];
    const restored = await this.commands.execute<HttpRequest[]>('bin.restore', { items: inputs });
    const binIds = new Set(inputs.map(i => i.bin_id));
    this.binnedRequests.update(list => list.filter(r => !binIds.has(r.id)));
    for (const req of restored ?? []) {
      await this.requestApi.restoreRequest(req);
    }
    return restored ?? [];
  }

  /** The collection a binned request goes back to, or null when it's gone. */
  originalCollectionName(req: HttpBinnedRequestSummary): string | null {
    return this.collectionApi.collections().find((c) => c.id === req.original_collection_id)?.name ?? null;
  }

  /**
   * Restores each request to its original collection. Returns false, having
   * restored nothing, when one's collection is gone and the user must pick.
   */
  async restoreToOriginal(binIds: number[]): Promise<boolean> {
    const binned = this.binnedRequests().filter((r) => binIds.includes(r.id));
    if (binned.some((r) => this.originalCollectionName(r) === null)) return false;
    await this.restoreMany(binned.map((r) => ({ bin_id: r.id, collection_id: r.original_collection_id })));
    return true;
  }

  async deletePermanently(binId: number): Promise<void> {
    return this.deleteManyPermanently([binId]);
  }

  async deleteManyPermanently(binIds: number[]): Promise<void> {
    if (binIds.length === 0) return;
    await this.commands.execute('bin.delete_permanently', { bin_ids: binIds });
    const idSet = new Set(binIds);
    this.binnedRequests.update(list => list.filter(r => !idSet.has(r.id)));
  }
}
