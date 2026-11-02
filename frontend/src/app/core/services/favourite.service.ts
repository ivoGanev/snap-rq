import { Injectable, inject, signal } from '@angular/core';
import { CommandApiService } from './command.service';
import type { HttpRequestSummary } from '../models/http';

export interface FavouriteAppearance {
  id: number;
  favourite_collection_id: number;
  appearance_type: string;
  appearance_value: string;
}

export interface FavouriteCollection {
  id: number;
  project_id: number;
  name: string;
  created_at: string;
  appearance: FavouriteAppearance;
}

export interface FavouriteItem {
  id: number;
  favourite_collection_id: number;
  http_request_id: number;
  user_sort_order: number;
  user_color: string | null;
}

/**
 * The app's typed API for favourites: every operation runs as a command.
 */
@Injectable({ providedIn: 'root' })
export class FavouriteApiService {
  private readonly commands = inject(CommandApiService);
  
  readonly collections = signal<FavouriteCollection[]>([]);
  readonly requests = signal<HttpRequestSummary[]>([]);
  readonly membership = signal<Set<number>>(new Set());
  readonly requestsMembership = signal<Record<number, number[]>>({});

  async loadCollectionsForProject(projectId: number): Promise<void> {
    const all = await this.commands.execute<FavouriteCollection[]>('favourite.list', { project_id: projectId });
    this.collections.set(all ?? []);
  }

  async createCollection(collection: Omit<FavouriteCollection, 'id' | 'created_at' | 'appearance'>): Promise<FavouriteCollection> {
    const created = await this.commands.execute<FavouriteCollection>('favourite.create', collection);
    await this.loadCollectionsForProject(collection.project_id);
    return created;
  }

  async updateCollection(collection: FavouriteCollection): Promise<FavouriteCollection> {
    const updated = await this.commands.execute<FavouriteCollection>('favourite.update', collection);
    this.collections.update(list => list.map(c => (c.id === updated.id ? updated : c)));
    return updated;
  }

  async updateAppearance(favouriteCollectionId: number, appearance: Omit<FavouriteAppearance, 'id' | 'favourite_collection_id'>): Promise<FavouriteAppearance> {
    const updated = await this.commands.execute<FavouriteAppearance>('favourite.set_appearance', {
      favourite_collection_id: favouriteCollectionId, ...appearance,
    });
    this.collections.update(list =>
      list.map(c => (c.id === favouriteCollectionId ? { ...c, appearance: updated } : c)),
    );
    return updated;
  }

  async deleteCollection(id: number): Promise<void> {
    await this.commands.execute('favourite.delete', { id });
    this.collections.update(list => list.filter(c => c.id !== id));
  }

  async loadRequestsForCollection(collectionId: number): Promise<void> {
    const all = await this.commands.execute<HttpRequestSummary[]>('favourite.list_requests', {
      favourite_collection_id: collectionId,
    });
    this.requests.set((all as HttpRequestSummary[] | null) ?? []);
  }

  async addRequest(collectionId: number, requestId: number): Promise<void> {
    await this.commands.execute('favourite.add_request', {
      favourite_collection_id: collectionId, request_id: requestId,
    });
  }

  async removeRequests(collectionId: number, requestIds: number[]): Promise<void> {
    if (requestIds.length === 0) return;
    await this.commands.execute('favourite.remove_requests', {
      favourite_collection_id: collectionId, request_ids: requestIds,
    });
  }

  async updateRequestOrder(collectionId: number, orderedRequestIds: number[]): Promise<void> {
    await this.commands.execute('favourite.reorder', {
      favourite_collection_id: collectionId, ordered_request_ids: orderedRequestIds,
    });
  }

  async loadMembershipForRequest(requestId: number): Promise<void> {
    const result = await this.commands.execute<Record<string, number[]>>('favourite.list_memberships', {
      request_ids: [requestId],
    });
    const all = result ?? {};
    const ids: number[] = all[`${requestId}`] ?? [];
    this.membership.set(new Set(ids.map(id => Number(id))));
  }

  /** Membership for many requests at once, keyed by request id. */
  async loadMembershipForRequests(requests: HttpRequestSummary[]): Promise<void> {
    if (requests.length === 0) {
      this.requestsMembership.set({});
      return;
    }
    const raw = await this.commands.execute<Record<string, number[]>>('favourite.list_memberships', {
      request_ids: requests.map(r => r.id),
    });
    const mapped: Record<number, number[]> = {};
    for (const [key, value] of Object.entries(raw ?? {})) {
      mapped[Number(key)] = value ?? [];
    }
    this.requestsMembership.set(mapped);
  }

  /** Sets (or clears with null) the colour on memberships in one collection. */
  async updateRequestColor(collectionId: number, requestIds: number[], color: string | null): Promise<void> {
    await this.commands.execute('favourite.set_color', {
      favourite_collection_id: collectionId, request_ids: requestIds, color,
    });
  }

  /** Removes one request from a collection (the single-item convenience). */
  async removeRequest(collectionId: number, requestId: number): Promise<void> {
    return this.removeRequests(collectionId, [requestId]);
  }

  clearMembership(): void {
    this.membership.set(new Set());
    this.requestsMembership.set({});
  }

}