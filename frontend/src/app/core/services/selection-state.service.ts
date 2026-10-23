import { Injectable } from '@angular/core';

export const groupKey = {
  collection: (collectionId: number) => `collection:${collectionId}`,
  favourite: (favouriteId: number) => `favourite:${favouriteId}`,
  tag: (projectId: number, tagName: string) => `tag:${projectId}:${tagName}`,
  allRequests: (projectId: number) => `all:${projectId}`,
  famous: (projectId: number) => `famous:${projectId}`,
  bin: (projectId: number) => `bin:${projectId}`,
};

/**
 * In-memory runtime state for the user's last selected request within each
 * request-list group (collection, favourite, tag, all requests, famous, bin).
 * This is intentionally not persisted across app restarts — it only preserves
 * selection state during the current session.
 *
 * A reverse index is maintained so that when a request is deleted we can
 * efficiently clear it from every group that had it selected.
 */
@Injectable({ providedIn: 'root' })
export class SelectionStateService {
  private readonly groupSelections = new Map<string, number>();
  private readonly requestToGroups = new Map<number, Set<string>>();

  getSelectedRequest(key: string): number | null {
    return this.groupSelections.get(key) ?? null;
  }

  setSelectedRequest(key: string, requestId: number | null): void {
    const previous = this.groupSelections.get(key);
    if (previous !== undefined) {
      const groups = this.requestToGroups.get(previous);
      groups?.delete(key);
      if (groups?.size === 0) {
        this.requestToGroups.delete(previous);
      }
    }

    if (requestId === null) {
      this.groupSelections.delete(key);
      return;
    }

    this.groupSelections.set(key, requestId);
    if (!this.requestToGroups.has(requestId)) {
      this.requestToGroups.set(requestId, new Set());
    }
    this.requestToGroups.get(requestId)!.add(key);
  }

  /**
   * Removes a request from all selection mappings. Called after a request is
   * deleted from the database so stale selections are cleared everywhere.
   */
  deleteRequest(requestId: number): void {
    const groups = this.requestToGroups.get(requestId);
    if (groups) {
      for (const key of groups) {
        this.groupSelections.delete(key);
      }
      this.requestToGroups.delete(requestId);
    }
  }
}
