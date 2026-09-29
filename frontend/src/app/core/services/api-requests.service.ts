import { Injectable, computed, inject, signal } from '@angular/core';
import * as RequestService from '../../../../bindings/snap-rq/backend/services';
import { RequestApiService, type HttpRequest, type HttpRequestSummary } from './request.service';
import { TagApiService } from './tag.service';
import { FavouriteApiService } from './favourite.service';

/**
 * In-memory store for all request summaries across every project.
 *
 * This is the single source of truth for the V2 list (and any future views).
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

  forCollection(collectionId: number): HttpRequestSummary[] {
    return this.allRequestsArray().filter(r => r.collection_id === collectionId);
  }

  all(): HttpRequestSummary[] {
    return this.allRequestsArray();
  }

  // ---------------------------------------------------------------------------
  // Dual-write CRUD
  // ---------------------------------------------------------------------------

  async create(req: Omit<HttpRequest, 'id'>): Promise<HttpRequest> {
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
    });
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
    await this.requestApi.update({ ...full, collection_id: collectionId, project_id: projectId });
    this.patch(requestId, { collection_id: collectionId, project_id: projectId });
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

  private remove(id: number): void {
    this.allRequests.update(map => {
      const next = { ...map };
      delete next[id];
      return next;
    });
  }

  private removeMany(ids: number[]): void {
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
