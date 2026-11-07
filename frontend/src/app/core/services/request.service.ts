import { Injectable, computed, inject, signal } from '@angular/core';
import { Events } from '@wailsio/runtime';
import { Subject } from 'rxjs';
import type { HttpRequest, HttpRequestSummary, HttpResponse } from '../models/http';
import type { EnvironmentVariableValue } from '../models/environment-context';
import { CommandApiService } from './command.service';
import { ChangeBus } from './change-bus.service';
import type { EntityChange } from '../models/change-set';
import { TagApiService } from './tag.service';
import { FavouriteApiService } from './favourite.service';

export type { HttpRequest, HttpResponse, HttpRequestSummary };

/**
 * The request service: the app's typed API for every request operation,
 * the in-memory store all views derive from, and the store sync for
 * command changes. One service, one store.
 */
@Injectable({ providedIn: 'root' })
export class RequestApiService {
  private readonly commands = inject(CommandApiService);
  private readonly bus = inject(ChangeBus);
  private readonly tagApi = inject(TagApiService);
  private readonly favouriteApi = inject(FavouriteApiService);

  // -----------------------------------------------------------------------
  // The store: every request summary, keyed by id
  // -----------------------------------------------------------------------

  readonly allRequests = signal<Record<number, HttpRequestSummary>>({});

  /** Sorted array view of the map. */
  readonly allRequestsArray = computed(() =>
    Object.values(this.allRequests()).sort((a, b) => a.name.localeCompare(b.name)),
  );

  /** Response history for the selected request. */
  readonly responses = signal<HttpResponse[]>([]);

  /** Elapsed ms per in-flight request id (send timers). */
  readonly sending = signal<Record<number, number>>({});

  /** Emits the saved response once per finished send. */
  readonly finished$ = new Subject<HttpResponse>();

  private readonly runToRequest = new Map<string, number>();
  private readonly activeRuns = new Map<number, Set<string>>();

  get(id: number): HttpRequestSummary | undefined {
    return this.allRequests()[id];
  }

  forProject(projectId: number): HttpRequestSummary[] {
    return this.allRequestsArray().filter(r => r.project_id === projectId);
  }

  /**
   * Requests of one collection in the user's custom order. "All requests"
   * stays name-sorted because that view is not user-orderable.
   */
  forCollection(collectionId: number): HttpRequestSummary[] {
    return this.allRequestsArray()
      .filter(r => r.collection_id === collectionId)
      .sort((a, b) => a.user_sort_order - b.user_sort_order || a.name.localeCompare(b.name));
  }

  all(): HttpRequestSummary[] {
    return this.allRequestsArray();
  }

  // -----------------------------------------------------------------------
  // Loading
  // -----------------------------------------------------------------------

  /** Loads every request summary into the store. Called once at startup. */
  async loadAll(): Promise<void> {
    const all = await this.commands.execute<HttpRequestSummary[]>('request.list_summaries', {});
    const map: Record<number, HttpRequestSummary> = {};
    for (const req of all ?? []) {
      map[req.id] = req;
    }
    this.allRequests.set(map);
  }

  async loadRecentRequests(projectId: number, limit = 50): Promise<HttpRequestSummary[]> {
    const result = await this.commands.execute<HttpRequestSummary[]>(
      'request.list_recent',
      { project_id: projectId, limit },
    );
    return result ?? [];
  }

  // -----------------------------------------------------------------------
  // API wrappers — every operation runs as a command
  // -----------------------------------------------------------------------

  async create(
    req: Omit<HttpRequest, 'id' | 'user_sort_order' | 'user_color' | 'last_used_at'>,
  ): Promise<HttpRequest> {
    const created = await this.commands.execute<HttpRequest>('request.create', req);
    this.addOrReplace(created);
    return created;
  }

  async duplicate(id: number): Promise<HttpRequest> {
    const duplicated = await this.commands.execute<HttpRequest>('request.duplicate', { id });
    this.addOrReplace(duplicated);
    return duplicated;
  }

  async getFull(id: number): Promise<HttpRequest> {
    return this.commands.execute<HttpRequest>('request.get', { id });
  }

  /** Targeted summary fetch; skips ids that no longer exist. */
  async getSummariesByIds(ids: number[]): Promise<HttpRequestSummary[]> {
    const result = await this.commands.execute<HttpRequestSummary[]>('request.list_by_ids', { ids });
    return result ?? [];
  }

  async update(req: HttpRequest): Promise<HttpRequest> {
    const updated = await this.commands.execute<HttpRequest>('request.update', req);
    this.patch(updated.id, {
      collection_id: updated.collection_id,
      project_id: updated.project_id,
      name: updated.name,
      url: updated.url,
      method: updated.method,
      status_code: updated.status_code,
      response_id: updated.response_id,
      user_sort_order: updated.user_sort_order,
      user_color: updated.user_color,
      last_used_at: updated.last_used_at,
    });
    return updated;
  }

  /** Moves a request to a different collection, appended to its order. */
  async move(requestId: number, collectionId: number, projectId: number): Promise<void> {
    const full = await this.getFull(requestId);
    const updated = await this.update({ ...full, collection_id: collectionId, project_id: projectId });
    this.patch(requestId, { user_sort_order: updated.user_sort_order });
  }

  async bulkUpdateBody(ids: number[], body: string, bodyType: string): Promise<void> {
    await this.commands.execute('request.bulk_set', { ids, field: 'body', value: body });
    await this.commands.execute('request.bulk_set', { ids, field: 'body_type', value: bodyType });
  }

  async bulkUpdateBodyForm(ids: number[], bodyForm: string): Promise<void> {
    await this.commands.execute('request.bulk_set', { ids, field: 'body_form', value: bodyForm });
  }

  async bulkUpdateBodyMode(ids: number[], bodyMode: string): Promise<void> {
    await this.commands.execute('request.bulk_set', { ids, field: 'body_mode', value: bodyMode });
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

  async bulkUpdateColor(ids: number[], color: string | null): Promise<void> {
    await this.commands.execute('request.bulk_set', { ids, field: 'color', value: color });
  }

  async delete(id: number): Promise<void> {
    return this.deleteMany([id]);
  }

  async deleteMany(ids: number[]): Promise<void> {
    if (ids.length === 0) return;
    await this.commands.execute('request.delete', { ids });
    this.removeMany(ids);
  }

  /** Persists the user's custom order within one collection. */
  async updateCollectionRequestOrder(collectionId: number, orderedRequestIds: number[]): Promise<void> {
    await this.commands.execute('request.update_order', {
      collection_id: collectionId,
      ordered_ids: orderedRequestIds,
    });
  }

  /** Brings a restored request back into the store with its memberships. */
  async restoreRequest(req: HttpRequestSummary): Promise<void> {
    this.addOrReplace(req);
    await Promise.all([
      this.tagApi.loadTagsForRequests([req]),
      this.favouriteApi.loadMembershipForRequests([req]),
    ]);
  }

  // -----------------------------------------------------------------------
  // Send flow
  // -----------------------------------------------------------------------

  /** Starts a send; progress and the saved response arrive via events. */
  async send(req: HttpRequest, variables: EnvironmentVariableValue[]): Promise<void> {
    const runId = await this.commands.begin('request.send', { request: req, variables });
    this.runToRequest.set(runId, req.id);
    const runs = this.activeRuns.get(req.id) ?? new Set<string>();
    runs.add(runId);
    this.activeRuns.set(req.id, runs);
    this.sending.update(s => ({ ...s, [req.id]: 0 }));
  }

  async loadResponsesForRequest(requestId: number): Promise<void> {
    const all = await this.commands.execute<HttpResponse[]>(
      'response.list_for_request',
      { request_id: requestId },
    );
    this.responses.set(all ?? []);
  }

  async saveResponseToFile(responseId: number, filePath: string): Promise<void> {
    await this.commands.execute('response.save_to_file', {
      response_id: responseId,
      file_path: filePath,
    });
  }

  // -----------------------------------------------------------------------
  // Curl helpers
  // -----------------------------------------------------------------------

  async requestToCurl(req: HttpRequest): Promise<string> {
    return this.commands.execute('curl.render', req);
  }

  async curlToRequest(collectionId: number, curl: string): Promise<HttpRequest> {
    return this.commands.execute('curl.parse', { collection_id: collectionId, command: curl });
  }

  // -----------------------------------------------------------------------
  // Store mutators
  // -----------------------------------------------------------------------

  addOrReplace(req: HttpRequestSummary): void {
    this.allRequests.update(map => ({ ...map, [req.id]: req }));
  }

  patch(id: number, changes: Partial<HttpRequestSummary>): void {
    this.allRequests.update(map => {
      const existing = map[id];
      if (!existing) return map;
      return { ...map, [id]: { ...existing, ...changes } };
    });
  }

  /** Drops the given requests in ONE signal write. */
  removeMany(ids: number[]): void {
    if (ids.length === 0) return;
    this.allRequests.update(map => {
      const next = { ...map };
      for (const id of ids) {
        delete next[id];
      }
      return next;
    });
  }

  /** Bulk-applies sort orders in ONE signal write (avoids O(n²) patching). */
  applyUserSortOrders(orders: Record<number, number>): void {
    const ids = Object.keys(orders);
    if (ids.length === 0) return;
    this.allRequests.update(map => {
      const next = { ...map };
      for (const key of ids) {
        const id = Number(key);
        const existing = next[id];
        if (existing) {
          next[id] = { ...existing, user_sort_order: orders[id] };
        }
      }
      return next;
    });
  }

  /** Bulk-applies colours in ONE signal write. */
  applyUserColors(colors: Record<number, string | null>): void {
    const ids = Object.keys(colors);
    if (ids.length === 0) return;
    this.allRequests.update(map => {
      const next = { ...map };
      for (const key of ids) {
        const id = Number(key);
        const existing = next[id];
        if (existing) {
          next[id] = { ...existing, user_color: colors[id] };
        }
      }
      return next;
    });
  }

  // -----------------------------------------------------------------------
  // Store sync — the ChangeBus tells us which request rows changed
  // -----------------------------------------------------------------------

  constructor() {
    this.bus.changesFor('request').subscribe(change => void this.applyChange(change));

    Events.On('command:update', ev => {
      const { runId, data } = ev.data as {
        runId: string;
        data?: { request_id?: number; elapsed_ms?: number };
      };
      const requestId = this.runToRequest.get(runId);
      const elapsed = data?.elapsed_ms;
      if (requestId !== undefined && elapsed !== undefined) {
        this.sending.update(s => ({ ...s, [requestId]: elapsed }));
      }
    });

    Events.On('command:finish', ev => {
      const { runId, result } = ev.data as { runId: string; result?: HttpResponse };
      const requestId = this.runToRequest.get(runId);
      if (requestId === undefined) return;

      this.runToRequest.delete(runId);
      const runs = this.activeRuns.get(requestId);
      runs?.delete(runId);
      if (!runs?.size) {
        this.activeRuns.delete(requestId);
        this.sending.update(s => {
          const next = { ...s };
          delete next[requestId];
          return next;
        });
      }

      if (result) {
        this.patch(requestId, { status_code: result.status_code, response_id: result.id });
        this.finished$.next(result);
      }
    });
  }

  /**
   * Fetches ONLY the touched rows by id and upserts/removes them. Existence
   * decides add-vs-remove, so execute, undo and redo all take one path.
   */
  private async applyChange(change: EntityChange): Promise<void> {
    const summaries = await this.getSummariesByIds(change.ids);
    const present = new Set(summaries.map(s => s.id));

    for (const s of summaries) {
      this.addOrReplace(s);
    }
    const gone = change.ids.filter(id => !present.has(id));
    if (gone.length > 0) {
      this.removeMany(gone);
    }
  }
}