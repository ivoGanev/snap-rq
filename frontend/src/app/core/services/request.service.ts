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

  async create(req: Omit<HttpRequest, 'id' | 'user_sort_order' | 'user_color'>): Promise<HttpRequest> {
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

  /** Targeted summary fetch for the store sync; skips ids that no longer exist. */
  async getSummariesByIds(ids: number[]): Promise<HttpRequestSummary[]> {
    const result = await RequestService.RequestService.GetRequestSummariesByIDs(ids);
    return result ?? [];
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

  async loadRecentRequests(projectId: number, limit = 50): Promise<HttpRequestSummary[]> {
    const result = await RequestService.RequestService.GetRecentRequests(projectId, limit);
    return result ?? [];
  }

  async recordRequestUsage(ids: number[]): Promise<void> {
    await RequestService.RequestService.BulkRecordRequestUsage(ids);
  }

  /** Persists the user's custom order of requests within one collection. */
  async updateCollectionRequestOrder(collectionId: number, orderedRequestIds: number[]): Promise<void> {
    await RequestService.RequestService.UpdateCollectionRequestOrder(collectionId, orderedRequestIds);
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
      user_sort_order: updated.user_sort_order,
      user_color: updated.user_color,
    });
    return updated;
  }

  async bulkUpdateBody(ids: number[], body: string, bodyType: string): Promise<HttpRequest[]> {
    const result = await RequestService.RequestService.BulkUpdateRequestBody(ids, body, bodyType);
    return result ?? [];
  }

  async bulkUpdateName(ids: number[], name: string): Promise<HttpRequest[]> {
    const result = await RequestService.RequestService.BulkUpdateRequestName(ids, name);
    return result ?? [];
  }

  async bulkUpdateUrl(ids: number[], url: string): Promise<HttpRequest[]> {
    const result = await RequestService.RequestService.BulkUpdateRequestUrl(ids, url);
    return result ?? [];
  }

  /** Sets (or clears with null) the colour on many requests in one call. */
  async bulkUpdateColor(ids: number[], color: string | null): Promise<HttpRequest[]> {
    const result = await RequestService.RequestService.BulkUpdateRequestColor(ids, color ?? '');
    return result ?? [];
  }

  async bulkUpdateMethod(ids: number[], method: string): Promise<HttpRequest[]> {
    const result = await RequestService.RequestService.BulkUpdateRequestMethod(ids, method);
    return result ?? [];
  }

  async bulkUpdateHeaders(ids: number[], headers: string): Promise<HttpRequest[]> {
    const result = await RequestService.RequestService.BulkUpdateRequestHeaders(ids, headers);
    return result ?? [];
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

  /**
   * Records a send's outcome (status code, newest response id) on the
   * request row. This is the send flow's owned write; user edits go through
   * update() and never touch these columns (column ownership).
   */
  async updateRequestResponse(id: number, statusCode: number, responseId: number): Promise<void> {
    await RequestService.RequestService.UpdateRequestResponse(id, statusCode, responseId);
  }

  async loadResponsesForRequest(requestId: number): Promise<void> {
    const all = await RequestService.RequestService.GetResponsesForRequest(requestId);
    this.responses.set(all ?? []);
  }

  async createResponse(resp: Omit<HttpResponse, 'id'>): Promise<HttpResponse> {
    return RequestService.RequestService.CreateResponse(resp as HttpResponse);
  }

  async bulkSaveResponses(responses: HttpResponse[]): Promise<HttpResponse[]> {
    const saved = await RequestService.RequestService.BulkSaveResponses(responses);
    return saved ?? [];
  }

  /** Executes the given request content; interpolation happens client-side. */
  async execute(req: HttpRequest): Promise<HttpResponse> {
    return RequestService.RequestService.ExecuteRequest(req);
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
