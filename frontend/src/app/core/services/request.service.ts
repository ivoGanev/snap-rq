import { Injectable, signal } from '@angular/core';
import * as RequestService from '../../../../bindings/snap-rq/backend/services';
import type { HttpRequest, HttpResponse, HttpRequestSummary } from '../../../../bindings/snap-rq/backend/models';

export type { HttpRequest, HttpResponse, HttpRequestSummary };

/**
 * Angular wrapper around the Wails-generated RequestService bindings.
 */
@Injectable({ providedIn: 'root' })
export class RequestApiService {
  /** Request list used by collection/all-request views (lightweight summary). */
  readonly requests = signal<HttpRequestSummary[]>([]);
  readonly responses = signal<HttpResponse[]>([]);

  /**
   * Inserts or replaces a request summary in the local list, keeping it sorted
   * by name. Used after create/duplicate/update instead of refetching the list.
   */
  addToRequests(req: HttpRequestSummary): void {
    this.requests.update(list => {
      const existing = list.findIndex(r => r.id === req.id);
      const next = existing === -1 ? [...list, req] : list.map((r, i) => (i === existing ? req : r));
      return next.sort((a, b) => a.name.localeCompare(b.name));
    });
  }

  /**
   * Removes a request from the local list by id. Used after delete instead of
   * refetching the list.
   */
  removeFromRequests(id: number): void {
    this.requests.update(list => list.filter(r => r.id !== id));
  }

  /**
   * Removes many requests from the local list in a single signal update.
   */
  removeManyFromRequests(ids: number[]): void {
    if (ids.length === 0) return;
    const idSet = new Set(ids);
    this.requests.update(list => list.filter(r => !idSet.has(r.id)));
  }

  /**
   * Applies a partial update to a request in the local list.
   */
  patchRequest(update: Partial<HttpRequestSummary> & { id: number }): void {
    this.requests.update(list =>
      list.map(r => (r.id === update.id ? { ...r, ...update } : r)),
    );
  }

  async create(req: Omit<HttpRequest, 'id'>): Promise<HttpRequest> {
    const created = await RequestService.RequestService.CreateRequest(req as HttpRequest);
    this.addToRequests(created);
    return created;
  }

  async duplicate(id: number): Promise<HttpRequest> {
    const duplicated = await RequestService.RequestService.DuplicateRequest(id);
    this.addToRequests(duplicated);
    return duplicated;
  }

  async get(id: number): Promise<HttpRequest> {
    return RequestService.RequestService.GetRequest(id);
  }

  async loadAll(): Promise<void> {
    const all = await RequestService.RequestService.GetAllRequestSummaries();
    this.requests.set(all ?? []);
  }

  async loadForCollection(collectionId: number): Promise<void> {
    const all = await RequestService.RequestService.GetRequestSummariesForCollection(collectionId);
    this.requests.set(all ?? []);
  }

  async loadForProject(projectId: number): Promise<void> {
    const all = await RequestService.RequestService.GetRequestSummariesForProject(projectId);
    this.requests.set(all ?? []);
  }

  async update(req: HttpRequest): Promise<HttpRequest> {
    const updated = await RequestService.RequestService.UpdateRequest(req);
    this.patchRequest({
      id: updated.id,
      collection_id: updated.collection_id,
      name: updated.name,
      url: updated.url,
      method: updated.method,
      status_code: updated.status_code,
      response_id: updated.response_id,
    });
    return updated;
  }

  async delete(id: number): Promise<void> {
    await RequestService.RequestService.DeleteRequest(id);
    this.removeFromRequests(id);
  }

  async deleteMany(ids: number[]): Promise<void> {
    if (ids.length === 0) return;
    await RequestService.RequestService.BulkDeleteRequests(ids);
    this.removeManyFromRequests(ids);
  }

  async loadResponsesForRequest(requestId: number): Promise<void> {
    const all = await RequestService.RequestService.GetResponsesForRequest(requestId);
    this.responses.set(all ?? []);
  }

  async createResponse(resp: Omit<HttpResponse, 'id'>): Promise<HttpResponse> {
    return RequestService.RequestService.CreateResponse(resp as HttpResponse);
  }

  async execute(requestId: number, environmentId: number): Promise<HttpResponse> {
    return RequestService.RequestService.ExecuteRequest(requestId, environmentId);
  }

  async requestToCurl(req: HttpRequest): Promise<string> {
    return RequestService.RequestService.RequestToCurl(req);
  }

  async curlToRequest(collectionId: number, curl: string): Promise<HttpRequest> {
    return RequestService.RequestService.CurlToRequest(collectionId, curl);
  }

  async saveResponseToFile(responseId: number, filePath: string): Promise<void> {
    await RequestService.RequestService.SaveResponseToFile(responseId, filePath);
  }
}
