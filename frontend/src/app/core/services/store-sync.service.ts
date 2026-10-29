import { Injectable, inject } from '@angular/core';
import { Subject, takeUntil } from 'rxjs';
import { CommandApiService, type CommandChange } from './command.service';
import { WorkspaceStateService } from './workspace-state.service';
import { ApiRequestsService } from './api-requests.service';
import { EnvironmentApiService } from './environment.service';
import { FavouriteApiService } from './favourite.service';

/**
 * Keeps the in-memory stores in step with committed commands. The backend
 * emits one 'command:changed' delta per commit (including undo/redo
 * replays); this service reloads the affected domain stores and bumps
 * dataRefresh so the active view reloads without clearing selection.
 *
 * This is the Phase 1.5 stand-in for the full event-driven store migration:
 * blunt (reload per entity) but correct, and it fixes the drift the ad-hoc
 * per-store patching could not see.
 */
@Injectable({ providedIn: 'root' })
export class StoreSyncService {
  private readonly commands = inject(CommandApiService);
  private readonly state = inject(WorkspaceStateService);
  private readonly apiRequests = inject(ApiRequestsService);
  private readonly environmentApi = inject(EnvironmentApiService);
  private readonly favouriteApi = inject(FavouriteApiService);

  private pendingEntities = new Set<string>();
  private flushTimer: number | undefined;
  private readonly destroy = new Subject<void>();

  constructor() {
    this.commands.change$.pipe(takeUntil(this.destroy)).subscribe(changes => {
      this.queue(changes);
    });
  }

  private queue(changes: CommandChange[]): void {
    for (const change of changes) {
      this.pendingEntities.add(change.entity);
    }
    if (this.pendingEntities.size === 0) return;

    // Debounce: one user action can emit several deltas in quick succession.
    if (this.flushTimer !== undefined) {
      window.clearTimeout(this.flushTimer);
    }
    this.flushTimer = window.setTimeout(() => void this.flush(), 50);
  }

  private async flush(): Promise<void> {
    this.flushTimer = undefined;
    const entities = this.pendingEntities;
    this.pendingEntities = new Set<string>();

    const project = this.state.selectedProject();
    try {
      for (const entity of entities) {
        switch (entity) {
          case 'request':
            // The all-requests store feeds the collection and all views;
            // the view-specific reload below refetches the active group.
            await this.apiRequests.loadAll();
            this.state.dataRefresh.update(n => n + 1);
            break;

          case 'favourite_collection':
            if (project) {
              await this.favouriteApi.loadCollectionsForProject(project.id);
            }
            this.state.dataRefresh.update(n => n + 1);
            break;

          case 'environment':
            if (project) {
              await this.environmentApi.loadForProject(project.id);
              if (!this.state.selectedEnvironment()) {
                this.state.selectedEnvironment.set(this.environmentApi.environments()[0] ?? null);
              }
            }
            break;
        }
      }
    } catch (err) {
      console.error('reloading stores after command change failed', err);
    }
  }
}
