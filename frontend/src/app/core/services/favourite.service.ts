import { Injectable, signal } from '@angular/core';
import * as FavouriteService from '../../../../bindings/snap-rq/backend/services';
import type { FavouriteCollection, FavouriteItem, FavouriteAppearance, HttpRequestSummary } from '../../../../bindings/snap-rq/backend/models';

export type { FavouriteCollection, FavouriteItem, FavouriteAppearance };

/**
 * Angular wrapper around the Wails-generated FavouriteService bindings.
 */
@Injectable({ providedIn: 'root' })
export class FavouriteApiService {
  readonly collections = signal<FavouriteCollection[]>([]);
  /** Favourite collection contents are displayed as list items (summary view). */
  readonly requests = signal<HttpRequestSummary[]>([]);
  readonly membership = signal<Set<number>>(new Set());
  /** Membership for many requests at once, keyed by request id. */
  readonly requestsMembership = signal<Record<number, number[]>>({});

  async createCollection(
    collection: Omit<FavouriteCollection, 'id' | 'created_at' | 'appearance'>,
  ): Promise<FavouriteCollection> {
    const created = await FavouriteService.FavouriteService.CreateFavouriteCollection(
      collection as FavouriteCollection,
    );
    await this.loadCollectionsForProject(collection.project_id);
    return created;
  }

  async loadCollectionsForProject(projectId: number): Promise<void> {
    const all = await FavouriteService.FavouriteService.GetFavouriteCollectionsForProject(projectId);
    this.collections.set(all ?? []);
  }

  async updateCollection(collection: FavouriteCollection): Promise<FavouriteCollection> {
    const updated = await FavouriteService.FavouriteService.UpdateFavouriteCollection(collection);
    this.collections.update(list =>
      list.map(c => (c.id === updated.id ? updated : c)),
    );
    return updated;
  }

  async updateAppearance(
    favouriteCollectionId: number,
    appearance: Omit<FavouriteAppearance, 'id' | 'favourite_collection_id'>,
  ): Promise<FavouriteAppearance> {
    const updated = await FavouriteService.FavouriteService.UpdateFavouriteAppearance(
      favouriteCollectionId,
      { ...appearance, id: 0, favourite_collection_id: favouriteCollectionId } as FavouriteAppearance,
    );
    this.collections.update(list =>
      list.map(c => (c.id === favouriteCollectionId ? { ...c, appearance: updated } : c)),
    );
    return updated;
  }

  async deleteCollection(id: number): Promise<void> {
    await FavouriteService.FavouriteService.DeleteFavouriteCollection(id);
    this.collections.update(list => list.filter(c => c.id !== id));
  }

  async loadRequestsForCollection(collectionId: number): Promise<void> {
    const all = await FavouriteService.FavouriteService.GetRequestsForFavouriteCollection(collectionId);
    this.requests.set((all as HttpRequestSummary[] | null) ?? []);
  }

  async loadMembershipForRequest(requestId: number): Promise<void> {
    const ids = await FavouriteService.FavouriteService.GetFavouriteCollectionIDsForRequest(requestId);
    this.membership.set(new Set((ids ?? []).map(id => Number(id))));
  }

  async addRequest(collectionId: number, requestId: number): Promise<FavouriteItem> {
    const item = await FavouriteService.FavouriteService.AddRequestToFavouriteCollection(collectionId, requestId);
    this.membership.update(set => new Set([...set, collectionId]));
    return item;
  }

  async removeRequest(collectionId: number, requestId: number): Promise<void> {
    await FavouriteService.FavouriteService.RemoveRequestFromFavouriteCollection(collectionId, requestId);
    this.membership.update(set => {
      const next = new Set(set);
      next.delete(collectionId);
      return next;
    });
  }

  clearMembership(): void {
    this.membership.set(new Set());
  }

  /**
   * Loads the favourite collection IDs for every request in the given list in a
   * single backend call. This avoids thousands of Wails round-trips when the V2
   * spreadsheet view loads a large request list.
   */
  async loadMembershipForRequests(requests: HttpRequestSummary[]): Promise<void> {
    if (requests.length === 0) {
      this.requestsMembership.set({});
      return;
    }

    const ids = requests.map(req => req.id);
    const raw = await FavouriteService.FavouriteService.GetFavouriteCollectionIDsForRequests(ids);
    const mapped: Record<number, number[]> = {};
    for (const [key, value] of Object.entries(raw ?? {})) {
      mapped[Number(key)] = (value ?? []).map(id => Number(id));
    }
    // Ensure every requested request has an entry even if the backend map shape
    // only includes requests with at least one favourite.
    for (const req of requests) {
      if (!(req.id in mapped)) {
        mapped[req.id] = [];
      }
    }
    this.requestsMembership.set(mapped);
  }
}
