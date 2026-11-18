import { Injectable } from '@angular/core';

export const groupKey = {
  collection: (collectionId: number) => `collection:${collectionId}`,
  favourite: (favouriteId: number) => `favourite:${favouriteId}`,
  tag: (projectId: number, tagName: string) => `tag:${projectId}:${tagName}`,
  allRequests: (projectId: number) => `all:${projectId}`,
  famous: (projectId: number) => `famous:${projectId}`,
  bin: (projectId: number) => `bin:${projectId}`,
};

/** What the user had selected in one list when they left it. */
export interface RememberedSelection {
  /** The row that carries the cell highlight and the scroll position. */
  primary: number;
  ids: number[];
}

/**
 * In-memory runtime state for the user's last selection within each
 * request-list group (collection, favourite, tag, all requests, famous, bin):
 * every selected request, and which one was the main one. This is
 * intentionally not persisted across app restarts — it only preserves
 * selection state during the current session.
 *
 * A reverse index is maintained so that when a request is deleted we can
 * efficiently remove it from every group that had it selected.
 */
@Injectable({ providedIn: 'root' })
export class SelectionStateService {
  private readonly groupSelections = new Map<string, RememberedSelection>();
  private readonly requestToGroups = new Map<number, Set<string>>();

  getSelection(key: string): RememberedSelection | null {
    return this.groupSelections.get(key) ?? null;
  }

  setSelection(key: string, primary: number, ids: Iterable<number>): void {
    this.clearSelection(key);
    const list = [...new Set([primary, ...ids])];
    this.groupSelections.set(key, { primary, ids: list });
    for (const id of list) {
      if (!this.requestToGroups.has(id)) {
        this.requestToGroups.set(id, new Set());
      }
      this.requestToGroups.get(id)!.add(key);
    }
  }

  clearSelection(key: string): void {
    const previous = this.groupSelections.get(key);
    if (!previous) return;
    for (const id of previous.ids) {
      const groups = this.requestToGroups.get(id);
      groups?.delete(key);
      if (groups?.size === 0) {
        this.requestToGroups.delete(id);
      }
    }
    this.groupSelections.delete(key);
  }

  /**
   * Removes a request from all remembered selections. Called after a request
   * is deleted from the database so stale selections are cleared everywhere.
   * A group whose main row was deleted falls back to its first remaining row.
   */
  deleteRequest(requestId: number): void {
    const groups = this.requestToGroups.get(requestId);
    if (!groups) return;
    for (const key of groups) {
      const selection = this.groupSelections.get(key);
      if (!selection) continue;
      const ids = selection.ids.filter((id) => id !== requestId);
      if (ids.length === 0) {
        this.groupSelections.delete(key);
      } else {
        this.groupSelections.set(key, {
          primary: ids.includes(selection.primary) ? selection.primary : ids[0],
          ids,
        });
      }
    }
    this.requestToGroups.delete(requestId);
  }
}
