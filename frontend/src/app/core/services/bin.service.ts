import { Injectable, inject, signal } from '@angular/core';
import type { HttpBinnedRequestSummary, HttpRequest } from '../models/http';
import { CommandApiService } from './command.service';
import { ApiRequestsService } from './api-requests.service';

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
  private readonly apiRequests = inject(ApiRequestsService);

  readonly binnedRequests = signal<HttpBinnedRequestSummary[]>([]);

  async loadForProject(projectId: number): Promise<void> {
    const all = await this.commands.execute<HttpBinnedRequestSummary[]>(
      'bin.list',
      { project_id: projectId },
    );
    this.binnedRequests.set(all ?? []);
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
      await this.apiRequests.restoreRequest(req);
    }
    return restored ?? [];
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
