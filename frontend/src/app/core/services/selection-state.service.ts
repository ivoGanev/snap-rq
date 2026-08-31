import { Injectable } from '@angular/core';

/**
 * In-memory runtime state for the user's last selected request within a
 * collection or favourite group. This is intentionally not persisted across
 * app restarts — it only preserves selection state during the current session.
 */
@Injectable({ providedIn: 'root' })
export class SelectionStateService {
  private readonly collectionSelections = new Map<number, number>();
  private readonly favouriteSelections = new Map<number, number>();

  getSelectedRequestForCollection(collectionId: number): number | null {
    return this.collectionSelections.get(collectionId) ?? null;
  }

  setSelectedRequestForCollection(collectionId: number, requestId: number | null): void {
    if (requestId === null) {
      this.collectionSelections.delete(collectionId);
    } else {
      this.collectionSelections.set(collectionId, requestId);
    }
  }

  getSelectedRequestForFavourite(favouriteId: number): number | null {
    return this.favouriteSelections.get(favouriteId) ?? null;
  }

  setSelectedRequestForFavourite(favouriteId: number, requestId: number | null): void {
    if (requestId === null) {
      this.favouriteSelections.delete(favouriteId);
    } else {
      this.favouriteSelections.set(favouriteId, requestId);
    }
  }
}
