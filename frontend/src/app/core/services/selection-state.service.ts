import { Injectable } from '@angular/core';

/**
 * In-memory runtime state for the user's last selected request within a
 * collection or favourite group. This is intentionally not persisted across
 * app restarts — it only preserves selection state during the current session.
 *
 * A reverse index is maintained so that when a request is deleted we can
 * efficiently clear it from every collection/favourite that had it selected.
 */
@Injectable({ providedIn: 'root' })
export class SelectionStateService {
  private readonly collectionSelections = new Map<number, number>();
  private readonly favouriteSelections = new Map<number, number>();
  private readonly requestToCollections = new Map<number, Set<number>>();
  private readonly requestToFavourites = new Map<number, Set<number>>();

  getSelectedRequestForCollection(collectionId: number): number | null {
    return this.collectionSelections.get(collectionId) ?? null;
  }

  setSelectedRequestForCollection(collectionId: number, requestId: number | null): void {
    const previous = this.collectionSelections.get(collectionId);
    if (previous !== undefined) {
      this.requestToCollections.get(previous)?.delete(collectionId);
      if (this.requestToCollections.get(previous)?.size === 0) {
        this.requestToCollections.delete(previous);
      }
    }

    if (requestId === null) {
      this.collectionSelections.delete(collectionId);
      return;
    }

    this.collectionSelections.set(collectionId, requestId);
    if (!this.requestToCollections.has(requestId)) {
      this.requestToCollections.set(requestId, new Set());
    }
    this.requestToCollections.get(requestId)!.add(collectionId);
  }

  getSelectedRequestForFavourite(favouriteId: number): number | null {
    return this.favouriteSelections.get(favouriteId) ?? null;
  }

  setSelectedRequestForFavourite(favouriteId: number, requestId: number | null): void {
    const previous = this.favouriteSelections.get(favouriteId);
    if (previous !== undefined) {
      this.requestToFavourites.get(previous)?.delete(favouriteId);
      if (this.requestToFavourites.get(previous)?.size === 0) {
        this.requestToFavourites.delete(previous);
      }
    }

    if (requestId === null) {
      this.favouriteSelections.delete(favouriteId);
      return;
    }

    this.favouriteSelections.set(favouriteId, requestId);
    if (!this.requestToFavourites.has(requestId)) {
      this.requestToFavourites.set(requestId, new Set());
    }
    this.requestToFavourites.get(requestId)!.add(favouriteId);
  }

  /**
   * Removes a request from all selection mappings. Called after a request is
   * deleted from the database so stale selections are cleared everywhere.
   */
  deleteRequest(requestId: number): void {
    const collections = this.requestToCollections.get(requestId);
    if (collections) {
      for (const collectionId of collections) {
        this.collectionSelections.delete(collectionId);
      }
      this.requestToCollections.delete(requestId);
    }

    const favourites = this.requestToFavourites.get(requestId);
    if (favourites) {
      for (const favouriteId of favourites) {
        this.favouriteSelections.delete(favouriteId);
      }
      this.requestToFavourites.delete(requestId);
    }
  }
}
