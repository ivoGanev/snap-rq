import { Injectable, computed, inject, signal } from '@angular/core';
import * as RequestService from '../../../../bindings/snap-rq/backend/services';
import { RequestApiService, type HttpRequest, type HttpRequestSummary } from './request.service';
import { TagApiService } from './tag.service';
import { FavouriteApiService } from './favourite.service';

/**
 * In-memory store for all request summaries across every project.
 *
 * This is the single source of truth for the request list.
 * The store is populated once at app startup from SQLite; after that every
 * mutation is written to the DB first, and only on success is the in-memory
 * map updated. Views never wait for a DB round-trip to refresh.
 *
 * Single-request full reads (GetRequest) still go straight to the backend, so
 * the request editor and execution flow are unaffected.
 */
@Injectable({ providedIn: 'root' })
export class ApiRequestsService {
  private readonly requestApi = inject(RequestApiService);
  private readonly tagApi = inject(TagApiService);
  private readonly favouriteApi = inject(FavouriteApiService);

  /** Every request summary keyed by id. Populated once on startup. */
  readonly allRequests = signal<Record<number, HttpRequestSummary>>({});

  /** Convenience sorted array view of the map. */
  readonly allRequestsArray = computed(() =>
    Object.values(this.allRequests()).sort((a, b) => a.name.localeCompare(b.name)),
  );

  // ---------------------------------------------------------------------------
  // One-time load
  // ---------------------------------------------------------------------------

  /**
   * Loads every request summary from SQLite into memory.
   * Called once when the app starts. Switching groups after this only filters
   * the map; it does not hit the database.
   */
  async loadAll(): Promise<void> {
    const all = await RequestService.RequestService.GetAllRequestSummaries();
    this.replaceAll(all ?? []);
  }

  // ---------------------------------------------------------------------------
  // Reads
  // ---------------------------------------------------------------------------

  get(id: number): HttpRequestSummary | undefined {
    return this.allRequests()[id];
  }

  forProject(projectId: number): HttpRequestSummary[] {
    return this.allRequestsArray().filter(r => r.project_id === projectId);
  }

  /**
   * Requests of one collection in the user's custom order. "All requests"
   * (allRequestsArray) stays name-sorted because that view is not
   * user-orderable; only collection views read the persisted order.
   */
  forCollection(collectionId: number): HttpRequestSummary[] {
    return this.allRequestsArray()
      .filter(r => r.collection_id === collectionId)
      .sort((a, b) => a.user_sort_order - b.user_sort_order || a.name.localeCompare(b.name));
  }

  all(): HttpRequestSummary[] {
    return this.allRequestsArray();
  }

  // ---------------------------------------------------------------------------
  // Dual-write CRUD
  // ---------------------------------------------------------------------------

  /**
   * Creates a request. `user_sort_order` and `user_color` are omitted from the
   * input because the backend assigns the order and new requests start uncoloured.
   */
  async create(req: Omit<HttpRequest, 'id' | 'user_sort_order' | 'user_color'>): Promise<HttpRequest> {
    const created = await this.requestApi.create(req as HttpRequest);
    this.addOrReplace(created);
    return created;
  }

  async duplicate(id: number): Promise<HttpRequest> {
    const duplicated = await this.requestApi.duplicate(id);
    this.addOrReplace(duplicated);
    return duplicated;
  }

  async update(req: HttpRequest): Promise<HttpRequest> {
    const updated = await this.requestApi.update(req);
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
    });
    return updated;
  }

  async bulkUpdateBody(ids: number[], body: string, bodyType: string): Promise<HttpRequest[]> {
    return this.applyBulkUpdate(this.requestApi.bulkUpdateBody(ids, body, bodyType));
  }

  async bulkUpdateName(ids: number[], name: string): Promise<HttpRequest[]> {
    return this.applyBulkUpdate(this.requestApi.bulkUpdateName(ids, name));
  }

  async bulkUpdateUrl(ids: number[], url: string): Promise<HttpRequest[]> {
    return this.applyBulkUpdate(this.requestApi.bulkUpdateUrl(ids, url));
  }

  async bulkUpdateMethod(ids: number[], method: string): Promise<HttpRequest[]> {
    return this.applyBulkUpdate(this.requestApi.bulkUpdateMethod(ids, method));
  }

  async bulkUpdateHeaders(ids: number[], headers: string): Promise<HttpRequest[]> {
    return this.applyBulkUpdate(this.requestApi.bulkUpdateHeaders(ids, headers));
  }

  private async applyBulkUpdate(pending: Promise<HttpRequest[]>): Promise<HttpRequest[]> {
    const updated = await pending;
    for (const req of updated) {
      this.patch(req.id, {
        collection_id: req.collection_id,
        project_id: req.project_id,
        name: req.name,
        url: req.url,
        method: req.method,
        status_code: req.status_code,
        response_id: req.response_id,
        user_sort_order: req.user_sort_order,
        user_color: req.user_color,
      });
    }
    return updated;
  }

  async delete(id: number): Promise<void> {
    await this.requestApi.delete(id);
    this.remove(id);
  }

  async deleteMany(ids: number[]): Promise<void> {
    await this.requestApi.deleteMany(ids);
    this.removeMany(ids);
  }

  async move(requestId: number, collectionId: number, projectId: number): Promise<void> {
    const full = await this.requestApi.get(requestId);
    const updated = await this.requestApi.update({ ...full, collection_id: collectionId, project_id: projectId });
    // The backend appends the request to the end of the target collection's
    // user order; keep the in-memory store in sync with the new position.
    this.patch(requestId, {
      collection_id: collectionId,
      project_id: projectId,
      user_sort_order: updated.user_sort_order,
      user_color: updated.user_color,
    });
  }

  // ---------------------------------------------------------------------------
  // Internal map mutators
  // ---------------------------------------------------------------------------

  addOrReplace(req: HttpRequestSummary): void {
    this.allRequests.update(map => ({ ...map, [req.id]: req }));
  }

  /**
   * Brings a restored request back into the in-memory stores and reloads its
   * tags and favourite memberships.
   */
  async restoreRequest(req: HttpRequestSummary): Promise<void> {
    this.requestApi.addToRequests(req);
    this.addOrReplace(req);
    await Promise.all([
      this.tagApi.loadTagsForRequests([req]),
      this.favouriteApi.loadMembershipForRequests([req]),
    ]);
  }

  patch(id: number, changes: Partial<HttpRequestSummary>): void {
    this.allRequests.update(map => {
      const existing = map[id];
      if (!existing) return map;
      return { ...map, [id]: { ...existing, ...changes } };
    });
  }

  /**
   * Bulk-applies new user_sort_order values in ONE signal write. Calling
   * patch() per request re-spreads the whole map N times (O(n²) with large
   * lists); this rebuilds it once.
   */
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

  /**
   * Bulk-applies user colours (null = clear) in ONE signal write, mirroring
   * applyUserSortOrders to avoid the O(n²) per-row patch churn.
   */
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

  private remove(id: number): void {
    this.allRequests.update(map => {
      const next = { ...map };
      delete next[id];
      return next;
    });
  }

  /** Drops the given requests from the store in ONE signal write. */
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

  private replaceAll(reqs: HttpRequestSummary[]): void {
    const map: Record<number, HttpRequestSummary> = {};
    for (const req of reqs) {
      map[req.id] = req;
    }
    this.allRequests.set(map);
  }
}
