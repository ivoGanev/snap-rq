import { Injectable, inject, signal } from '@angular/core';
import type { HttpRequestSummary } from '../models/http';
import { RequestApiService } from './request.service';
import { FavouriteApiService } from './favourite.service';
import { TagApiService } from './tag.service';
import { CollectionApiService } from './collection.service';
import { WorkspaceStateService } from './workspace-state.service';
import { ToastService } from './toast.service';

export type DropKind = 'collection' | 'favourite' | 'tag';

export interface DropTarget {
  kind: DropKind;
  id: number;
  name: string;
}

export interface DragSession {
  requestIds: number[];
  request: HttpRequestSummary;
}

/**
 * Cross-component drag session. The request list is the drag source and keeps
 * rendering the ghost; the sidebar reports which target (or empty collections
 * panel) is under the pointer. On release the list calls `tryDrop`, which runs
 * the relevant organize command here so both surfaces share one code path.
 */
@Injectable({ providedIn: 'root' })
export class DragSessionService {
  private readonly requestApi = inject(RequestApiService);
  private readonly favouriteApi = inject(FavouriteApiService);
  private readonly tagApi = inject(TagApiService);
  private readonly collectionApi = inject(CollectionApiService);
  private readonly state = inject(WorkspaceStateService);
  private readonly toast = inject(ToastService);

  readonly active = signal<DragSession | null>(null);
  readonly hovered = signal<DropTarget | null>(null);
  readonly overEmptyCollections = signal(false);

  readonly createCollectionRequested = signal(false);
  private pendingRequestIds: number[] = [];
  private pendingProjectId: number | null = null;

  start(session: DragSession): void {
    this.active.set(session);
  }

  setHover(target: DropTarget | null): void {
    this.hovered.set(target);
    this.overEmptyCollections.set(false);
  }

  setOverEmptyCollections(over: boolean): void {
    this.overEmptyCollections.set(over);
    if (over) {
      this.hovered.set(null);
    }
  }

  /**
   * Resolves the current pointer location into an action. Returns true when
   * the drop was consumed by the sidebar (so the caller skips list reorder).
   */
  tryDrop(): boolean {
    const session = this.active();
    if (!session) return false;

    const target = this.hovered();
    if (target) {
      void this.applyDrop(session, target);
      return true;
    }

    if (this.overEmptyCollections()) {
      const project = this.state.selectedProject();
      if (!project) return false;
      this.pendingRequestIds = session.requestIds;
      this.pendingProjectId = project.id;
      this.createCollectionRequested.set(true);
      return true;
    }

    return false;
  }

  end(): void {
    this.active.set(null);
    this.hovered.set(null);
    this.overEmptyCollections.set(false);
  }

  cancelCreateCollection(): void {
    this.pendingRequestIds = [];
    this.pendingProjectId = null;
    this.createCollectionRequested.set(false);
  }

  async confirmCreateCollection(name: string): Promise<void> {
    const projectId = this.pendingProjectId;
    const requestIds = this.pendingRequestIds;
    this.cancelCreateCollection();
    if (!projectId || requestIds.length === 0) return;

    try {
      const collection = await this.collectionApi.create({ project_id: projectId, name });
      for (const id of requestIds) {
        await this.requestApi.move(id, collection.id, projectId);
      }
      this.toast.show(
        requestIds.length === 1
          ? `Moved to "${collection.name}"`
          : `Moved ${requestIds.length} requests to "${collection.name}"`,
      );
    } catch (err) {
      console.error('creating collection and moving request failed', err);
    }
  }

  private async applyDrop(session: DragSession, target: DropTarget): Promise<void> {
    const name = session.request.name;
    const requestId = session.request.id;

    if (target.kind === 'collection') {
      const project = this.state.selectedProject();
      if (!project) return;
      if (session.request.collection_id === target.id) {
        this.toast.show(`Already in "${target.name}"`);
        return;
      }
      try {
        await this.requestApi.move(requestId, target.id, project.id);
        this.toast.show(`Moved "${name}" to "${target.name}"`);
      } catch (err) {
        console.error('moving request to collection failed', err);
      }
      return;
    }

    if (target.kind === 'favourite') {
      try {
        const alreadyMember = await this.favouriteApi.isMember(target.id, requestId);
        if (alreadyMember) {
          this.toast.show(`Already in "${target.name}"`);
          return;
        }
        await this.favouriteApi.addRequest(target.id, requestId);
        this.toast.show(`Added "${name}" to "${target.name}"`);
      } catch (err) {
        console.error('adding request to favourite failed', err);
      }
      return;
    }

    if (target.kind === 'tag') {
      const project = this.state.selectedProject();
      if (!project) return;
      const existing = this.tagApi.requestTags()[requestId] ?? [];
      if (existing.includes(target.name)) {
        this.toast.show(`Already tagged "${target.name}"`);
        return;
      }
      try {
        await this.tagApi.addTagToRequest(requestId, project.id, target.name);
        this.toast.show(`Tagged "${name}" with "${target.name}"`);
      } catch (err) {
        console.error('adding tag to request failed', err);
      }
    }
  }
}
