import { Injectable, inject, signal } from '@angular/core';
import * as RequestService from '../../../../bindings/snap-rq/backend/services';
import type {
  HttpBinnedRequestSummary,
  HttpRequest,
  RestoreBinnedRequestInput,
} from '../../../../bindings/snap-rq/backend/models';
import { ApiRequestsService } from './api-requests.service';

export type { HttpBinnedRequestSummary, RestoreBinnedRequestInput };

/**
 * Angular wrapper around the binned/trash-bin request bindings.
 */
@Injectable({ providedIn: 'root' })
export class BinApiService {
  private readonly apiRequests = inject(ApiRequestsService);

  readonly binnedRequests = signal<HttpBinnedRequestSummary[]>([]);

  async loadForProject(projectId: number): Promise<void> {
    const all = await RequestService.RequestService.GetBinnedRequestsForProject(projectId);
    this.binnedRequests.set(all ?? []);
  }

  clear(): void {
    this.binnedRequests.set([]);
  }

  async restore(binId: number, targetCollectionId: number): Promise<HttpRequest> {
    const restored = await RequestService.RequestService.RestoreBinnedRequest(binId, targetCollectionId);
    this.remove(binId);
    await this.apiRequests.restoreRequest(restored);
    return restored;
  }

  async restoreMany(inputs: RestoreBinnedRequestInput[]): Promise<HttpRequest[]> {
    if (inputs.length === 0) return [];
    const restored = await RequestService.RequestService.RestoreBinnedRequests(inputs);
    const binIds = new Set(inputs.map(i => i.bin_id));
    this.binnedRequests.update(list => list.filter(r => !binIds.has(r.id)));
    for (const req of restored ?? []) {
      await this.apiRequests.restoreRequest(req);
    }
    return restored ?? [];
  }

  async deletePermanently(binId: number): Promise<void> {
    await RequestService.RequestService.PermanentlyDeleteBinnedRequest(binId);
    this.remove(binId);
  }

  async deleteManyPermanently(binIds: number[]): Promise<void> {
    if (binIds.length === 0) return;
    await RequestService.RequestService.PermanentlyDeleteBinnedRequests(binIds);
    const idSet = new Set(binIds);
    this.binnedRequests.update(list => list.filter(r => !idSet.has(r.id)));
  }

  private remove(binId: number): void {
    this.binnedRequests.update(list => list.filter(r => r.id !== binId));
  }
}
