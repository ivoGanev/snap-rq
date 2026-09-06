import { Injectable, signal } from '@angular/core';
import * as FavouriteService from '../../../../bindings/snap-rq/backend/services';
import type { FavouriteCollection, FavouriteItem, HttpRequest } from '../../../../bindings/snap-rq/backend/models';

export type { FavouriteCollection, FavouriteItem };

/**
 * Angular wrapper around the Wails-generated FavouriteService bindings.
 */
@Injectable({ providedIn: 'root' })
export class FavouriteApiService {
  readonly collections = signal<FavouriteCollection[]>([]);
  readonly requests = signal<HttpRequest[]>([]);
  readonly membership = signal<Set<number>>(new Set());
  /** Membership for many requests at once, keyed by request id. */
  readonly requestsMembership = signal<Record<number, number[]>>({});

  async createCollection(collection: Omit<FavouriteCollection, 'id' | 'created_at'>): Promise<FavouriteCollection> {
    const created = await FavouriteService.FavouriteService.CreateFavouriteCollection(collection as FavouriteCollection);
    await this.loadCollectionsForProfile(collection.profile_id);
    return created;
  }

  async loadCollectionsForProfile(profileId: number): Promise<void> {
    const all = await FavouriteService.FavouriteService.GetFavouriteCollectionsForProfile(profileId);
    this.collections.set(all ?? []);
  }

  async updateCollection(collection: FavouriteCollection): Promise<FavouriteCollection> {
    const updated = await FavouriteService.FavouriteService.UpdateFavouriteCollection(collection);
    this.collections.update(list =>
      list.map(c => (c.id === updated.id ? updated : c)),
    );
    return updated;
  }

  async deleteCollection(id: number): Promise<void> {
    await FavouriteService.FavouriteService.DeleteFavouriteCollection(id);
    this.collections.update(list => list.filter(c => c.id !== id));
  }

  async loadRequestsForCollection(collectionId: number): Promise<void> {
    const all = await FavouriteService.FavouriteService.GetRequestsForFavouriteCollection(collectionId);
    this.requests.set(all ?? []);
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
   * Loads the favourite collection IDs for every request in the given list.
   * This is used by the V2 spreadsheet view to display/sort the Favourites
   * column without forcing a separate round-trip per visible row on every
   * interaction.
   */
  async loadMembershipForRequests(requests: HttpRequest[]): Promise<void> {
    if (requests.length === 0) {
      this.requestsMembership.set({});
      return;
    }

    const map: Record<number, number[]> = {};
    await Promise.all(
      requests.map(async (req) => {
        try {
          const ids = await FavouriteService.FavouriteService.GetFavouriteCollectionIDsForRequest(
            req.id,
          );
          map[req.id] = (ids ?? []).map((id) => Number(id));
        } catch (err) {
          console.error(err);
          map[req.id] = [];
        }
      }),
    );
    this.requestsMembership.set(map);
  }
}
