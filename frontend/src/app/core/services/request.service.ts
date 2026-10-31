import { Injectable, inject, signal } from '@angular/core';
import type { HttpRequest, HttpRequestSummary, HttpResponse } from '../models/http';
import { CommandApiService } from './command.service';

export type { HttpRequest, HttpResponse, HttpRequestSummary };

/**
 * The app's typed API for requests: every operation runs as a command
 * through the backend engine (see specs/command-framework.md), so edits,
 * deletes and creates are undoable and sends never block a writer.
 */
@Injectable({ providedIn: 'root' })
export class RequestApiService {
  private readonly commands = inject(CommandApiService);

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
    const created = await this.commands.execute<HttpRequest>('request.create', req);
    this.addToRequests(created);
    return created;
  }

  async duplicate(id: number): Promise<HttpRequest> {
    const duplicated = await this.commands.execute<HttpRequest>('request.duplicate', { id });
    this.addToRequests(duplicated);
    return duplicated;
  }

  async get(id: number): Promise<HttpRequest> {
    return this.commands.execute<HttpRequest>('request.get', { id });
  }

  /** Targeted summary fetch for the store sync; skips ids that no longer exist. */
  async getSummariesByIds(ids: number[]): Promise<HttpRequestSummary[]> {
    const result = await this.commands.execute<HttpRequestSummary[]>('request.list_by_ids', { ids });
    return result ?? [];
  }

  async loadAll(): Promise<void> {
    const all = await this.commands.execute<HttpRequestSummary[]>('request.list_summaries', {});
    this.requests.set(all ?? []);
  }

  async loadForCollection(collectionId: number): Promise<void> {
    const all = await this.commands.execute<HttpRequestSummary[]>(
      'request.list_for_collection',
      { collection_id: collectionId },
    );
    this.requests.set(all ?? []);
  }

  async loadForProject(projectId: number): Promise<void> {
    const all = await this.commands.execute<HttpRequestSummary[]>(
      'request.list_for_project',
      { project_id: projectId },
    );
    this.requests.set(all ?? []);
  }

  async loadRecentRequests(projectId: number, limit = 50): Promise<HttpRequestSummary[]> {
    const result = await this.commands.execute<HttpRequestSummary[]>(
      'request.list_recent',
      { project_id: projectId, limit },
    );
    return result ?? [];
  }

  async recordRequestUsage(ids: number[]): Promise<void> {
    await this.commands.execute('request.record_usage', { ids });
  }

  /** Persists the user's custom order of requests within one collection. */
  async updateCollectionRequestOrder(collectionId: number, orderedRequestIds: number[]): Promise<void> {
    await this.commands.execute('request.update_order', {
      collection_id: collectionId,
      ordered_ids: orderedRequestIds,
    });
  }

  async update(req: HttpRequest): Promise<HttpRequest> {
    const updated = await this.commands.execute<HttpRequest>('request.update', req);
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

  async bulkUpdateBody(ids: number[], body: string, bodyType: string): Promise<void> {
    // Two quick runs of the same command on the same rows: the engine
    // merges them into one undo step.
    await this.commands.execute('request.bulk_set', { ids, field: 'body', value: body });
    await this.commands.execute('request.bulk_set', { ids, field: 'body_type', value: bodyType });
  }

  async bulkUpdateName(ids: number[], name: string): Promise<void> {
    await this.commands.execute('request.bulk_set', { ids, field: 'name', value: name });
  }

  async bulkUpdateUrl(ids: number[], url: string): Promise<void> {
    await this.commands.execute('request.bulk_set', { ids, field: 'url', value: url });
  }

  async bulkUpdateMethod(ids: number[], method: string): Promise<void> {
    await this.commands.execute('request.bulk_set', { ids, field: 'method', value: method });
  }

  async bulkUpdateHeaders(ids: number[], headers: string): Promise<void> {
    await this.commands.execute('request.bulk_set', { ids, field: 'headers', value: headers });
  }

  /** Sets (or clears with null) the colour on many requests in one call. */
  async bulkUpdateColor(ids: number[], color: string | null): Promise<void> {
    await this.commands.execute('request.bulk_set', { ids, field: 'color', value: color });
  }

  async delete(id: number): Promise<void> {
    return this.deleteMany([id]);
  }

  async deleteMany(ids: number[]): Promise<void> {
    if (ids.length === 0) return;
    await this.commands.execute('request.delete', { ids });
    this.removeManyFromRequests(ids);
  }

  async loadResponsesForRequest(requestId: number): Promise<void> {
    const all = await this.commands.execute<HttpResponse[]>(
      'response.list_for_request',
      { request_id: requestId },
    );
    this.responses.set(all ?? []);
  }

  async createResponse(resp: Omit<HttpResponse, 'id'>): Promise<HttpResponse> {
    return this.commands.execute('response.save', resp);
  }

  async bulkSaveResponses(responses: HttpResponse[]): Promise<HttpResponse[]> {
    const saved = await this.commands.execute<HttpResponse[]>('response.save_bulk', { responses });
    return saved ?? [];
  }

  /** Executes the given request content; interpolation happens client-side. */
  async execute(req: HttpRequest): Promise<HttpResponse> {
    return this.commands.execute('request.send', req);
  }

  async requestToCurl(req: HttpRequest): Promise<string> {
    return this.commands.execute('curl.render', req);
  }

  async curlToRequest(collectionId: number, curl: string): Promise<HttpRequest> {
    return this.commands.execute('curl.parse', { collection_id: collectionId, command: curl });
  }

  async saveResponseToFile(responseId: number, filePath: string): Promise<void> {
    await this.commands.execute('response.save_to_file', {
      response_id: responseId,
      file_path: filePath,
    });
  }

  /**
   * Records a send's outcome (status code, newest response id) on the
   * request row. The send flow's owned write; user edits never touch these
   * columns, so an undo can never clobber a send's result.
   */
  async updateRequestResponse(id: number, statusCode: number, responseId: number): Promise<void> {
    await this.commands.execute('request.record_response', {
      request_id: id,
      status_code: statusCode,
      response_id: responseId,
    });
  }
}
