import { Injectable, computed, inject, signal } from '@angular/core';
import { Subject } from 'rxjs';
import type { HttpRequest, HttpRequestSummary, HttpResponse, RequestUsage } from '../models/http';
import type { EnvironmentVariableValue } from '../models/environment-context';
import { CurlService } from '../../../../bindings/snap-rq/backend/commands/requests';
import { CommandApiService } from './command.service';
import { ChangeBus } from './change-bus.service';
import { ToastService } from './toast.service';
import type { EntityChange } from '../models/change-set';
import { ScriptApiService } from './script.service';
import { FavouriteApiService } from './favourite.service';
import { CollectionApiService } from './collection.service';

export type { HttpRequest, HttpResponse, HttpRequestSummary };

/** Name of a request created with "+ Request"; the user renames it right away. */
const NEW_REQUEST_NAME = 'My new snappy API';

/**
 * The request service: the app's typed API for every request operation,
 * the in-memory store all views derive from, and the store sync for
 * command changes. One service, one store.
 */
@Injectable({ providedIn: 'root' })
export class RequestApiService {
  private readonly commands = inject(CommandApiService);
  private readonly bus = inject(ChangeBus);
  private readonly toasts = inject(ToastService);
  private readonly scriptApi = inject(ScriptApiService);
  private readonly favouriteApi = inject(FavouriteApiService);
  private readonly collectionApi = inject(CollectionApiService);

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

  /** When the latest send of each request in flight started (ms since epoch). */
  readonly sending = signal<Record<number, number>>({});

  /** How much the project's requests are used lately, by id (absent = no usage). */
  readonly usage = signal<Record<number, RequestUsage>>({});

  /** Emits the saved response once per finished send. */
  readonly finished$ = new Subject<HttpResponse>();

  /** Sends in flight per request id (sending again while in flight is fine). */
  private readonly inFlight = new Map<number, Set<AbortController>>();

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

  /** Loads the project's usage (levels fade over days, so reload now and then). */
  async loadUsage(projectId: number): Promise<void> {
    const list = await this.commands.execute<RequestUsage[]>('request.list_usage', { project_id: projectId });
    const byId: Record<number, RequestUsage> = {};
    for (const usage of list ?? []) byId[usage.id] = usage;
    this.usage.set(byId);
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

  /** A new empty GET request in the collection, with the default name. */
  async createBlank(collection: { id: number; project_id: number }): Promise<HttpRequest> {
    return this.create({
      collection_id: collection.id,
      project_id: collection.project_id,
      name: NEW_REQUEST_NAME,
      url: '',
      method: 'GET',
      body: '',
      body_type: 'json',
      body_form: '',
      request_headers: '',
      status_code: 0,
      response_id: 0,
    });
  }

  /** A new request in the collection from a cURL command; throws if it can't be parsed. */
  async createFromCurl(collection: { id: number; project_id: number }, curl: string): Promise<HttpRequest> {
    const parsed = await this.curlToRequest(collection.id, curl);
    return this.create({
      collection_id: collection.id,
      project_id: collection.project_id,
      name: parsed.name,
      url: parsed.url,
      method: parsed.method,
      body: parsed.body,
      body_type: parsed.body_type || 'json',
      body_form: parsed.body_form || '',
      request_headers: parsed.request_headers,
      status_code: 0,
      response_id: 0,
    });
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
      this.scriptApi.loadScriptsForRequests([req]),
      this.favouriteApi.loadMembershipForRequests([req]),
    ]);
  }

  // -----------------------------------------------------------------------
  // Send flow
  // -----------------------------------------------------------------------

  /**
   * Sends a request and resolves once its response is saved; secret
   * variables are filled in by the backend from `environmentId`. The request
   * is in flight until its run finishes, whatever the outcome.
   */
  async send(req: HttpRequest, variables: EnvironmentVariableValue[], environmentId: number | null): Promise<void> {
    const sending = this.startSending(req.id);
    try {
      const result = await this.commands.run<HttpResponse>(
        'request.send',
        { request: req, variables, environment_id: environmentId ?? 0 },
        undefined,
        sending.signal,
      );
      // last_used_at as the backend stores it (Unix ns), so the smart views
      // (Recent, Failing, Not used lately) move the request straight away.
      const sentAt = Date.now() * 1e6;
      this.patch(req.id, {
        status_code: result.status_code,
        response_id: result.id,
        last_used_at: sentAt,
      });
      this.collectionApi.collections.update((list) =>
        list.map((c) => (c.id === req.collection_id ? { ...c, last_used_at: sentAt } : c)),
      );
      void this.loadUsage(req.project_id);
      this.finished$.next(result);
    } catch (err) {
      if (sending.signal.aborted) {
        this.toasts.show('Request cancelled');
      } else {
        // A send that couldn't run, e.g. a locked secret variable.
        this.toasts.show(err instanceof Error ? err.message : String(err));
      }
    } finally {
      this.stopSending(req.id, sending);
    }
  }

  /** Stops every send of the request still in flight; none of them saves a response. */
  cancel(requestId: number): void {
    for (const sending of this.inFlight.get(requestId) ?? []) sending.abort();
  }

  private startSending(id: number): AbortController {
    const sending = new AbortController();
    const all = this.inFlight.get(id) ?? new Set<AbortController>();
    all.add(sending);
    this.inFlight.set(id, all);
    this.sending.update(s => ({ ...s, [id]: Date.now() }));
    return sending;
  }

  private stopSending(id: number, sending: AbortController): void {
    const all = this.inFlight.get(id);
    all?.delete(sending);
    if (all?.size) return;
    this.inFlight.delete(id);
    this.sending.update(({ [id]: _, ...rest }) => rest);
  }

  async loadResponsesForRequest(requestId: number): Promise<void> {
    const all = await this.commands.execute<HttpResponse[]>(
      'response.list_for_request',
      { request_id: requestId },
    );
    this.responses.set(all ?? []);
  }

  /** Deletes a request's whole response history; its status goes with it. */
  async clearResponses(requestId: number): Promise<void> {
    await this.commands.execute('response.clear_for_request', { request_id: requestId });
    this.responses.set([]);
    this.patch(requestId, { status_code: 0, response_id: 0 });
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

  /**
   * The request as a curl command with the environment's variables filled in,
   * secrets included (the backend has their values); unknown ones stay {{key}}.
   */
  async requestToCurl(
    req: HttpRequest,
    variables: { key: string; value: string }[],
    environmentId: number | null,
  ): Promise<string> {
    const plain = Object.fromEntries(variables.map((v) => [v.key, v.value]));
    return CurlService.Render(req, plain, environmentId ?? 0);
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