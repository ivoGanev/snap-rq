import { Injectable, inject } from '@angular/core';
import { Subject, takeUntil } from 'rxjs';
import { CommandApiService, type CommandChange } from './command.service';
import { WorkspaceStateService } from './workspace-state.service';
import { ApiRequestsService } from './api-requests.service';
import { EnvironmentApiService } from './environment.service';
import { EnvironmentVariableApiService } from './environment-variable.service';
import { FavouriteApiService } from './favourite.service';
import { RequestApiService } from './request.service';

/**
 * Keeps the in-memory stores in step with committed commands. The backend
 * emits one 'command:changed' delta per commit (including undo/redo
 * replays); this service applies them WITHOUT full reloads — the request
 * store is refreshed by id (rows that vanished are dropped, rows that exist
 * are upserted), and only view-local stores (favourite/tag/recent/bin
 * lists) trigger their scoped reload via dataRefresh.
 *
 * Undo/redo must feel instant: no fixed debounce, first delta flushes
 * immediately, and deltas arriving during a flush merge into the next one.
 */
@Injectable({ providedIn: 'root' })
export class StoreSyncService {
  private readonly commands = inject(CommandApiService);
  private readonly state = inject(WorkspaceStateService);
  private readonly apiRequests = inject(ApiRequestsService);
  private readonly requestApi = inject(RequestApiService);
  private readonly environmentApi = inject(EnvironmentApiService);
  private readonly envVarApi = inject(EnvironmentVariableApiService);
  private readonly favouriteApi = inject(FavouriteApiService);

  private pending: CommandChange[] = [];
  private flushing = false;
  private readonly destroy = new Subject<void>();

  constructor() {
    this.commands.change$.pipe(takeUntil(this.destroy)).subscribe(changes => {
      this.queue(changes);
    });
  }

  private queue(changes: CommandChange[]): void {
    if (changes.length === 0) return;
    this.pending.push(...changes);
    if (this.flushing) return; // merges into the flush already in flight
    void this.flush();
  }

  private async flush(): Promise<void> {
    this.flushing = true;
    try {
      while (this.pending.length > 0) {
        const batch = this.pending;
        this.pending = [];
        await this.apply(batch);
      }
    } finally {
      this.flushing = false;
    }
  }

  private async apply(changes: CommandChange[]): Promise<void> {
    const entities = new Set(changes.map(c => c.entity));

    try {
      if (entities.has('request')) {
        await this.refreshRequests(changes.filter(c => c.entity === 'request'));
      }
      if (entities.has('favourite_collection')) {
        const project = this.state.selectedProject();
        if (project) {
          await this.favouriteApi.loadCollectionsForProject(project.id);
        }
        this.state.dataRefresh.update(n => n + 1);
      }
      if (entities.has('environment')) {
        const project = this.state.selectedProject();
        if (project) {
          await this.environmentApi.loadForProject(project.id);
          if (!this.state.selectedEnvironment()) {
            this.state.selectedEnvironment.set(this.environmentApi.environments()[0] ?? null);
          }
        }
      }
      if (entities.has('environment_variable')) {
        const env = this.state.selectedEnvironment();
        if (env) {
          await this.envVarApi.loadForEnvironment(env.id);
        }
      }
    } catch (err) {
      console.error('syncing stores after command change failed', err);
    }
  }

  /**
   * Existence-based refresh, so the same code covers execute, undo and redo
   * without op inversion: every touched id is re-fetched; ids that come
   * back are upserted, ids that vanished are dropped. Collection and all
   * views derive from the store map and update instantly; view-local lists
   * (favourite/tag/recent/bin) get their scoped reload.
   */
  private async refreshRequests(requestChanges: CommandChange[]): Promise<void> {
    const ids = [...new Set(requestChanges.flatMap(c => c.ids))];
    if (ids.length > 0) {
      const summaries = await this.requestApi.getSummariesByIds(ids);
      const present = new Set(summaries.map(s => s.id));

      for (const summary of summaries) {
        this.apiRequests.addOrReplace(summary);
        this.requestApi.addToRequests(summary);
      }
      const gone = ids.filter(id => !present.has(id));
      if (gone.length > 0) {
        this.apiRequests.removeMany(gone);
        this.requestApi.removeManyFromRequests(gone);
      }
    }

    const viewIsLocalList =
      this.state.selectedFavouriteCollection() !== null ||
      this.state.selectedTag() !== null ||
      this.state.showingFamousRequests() ||
      this.state.showingBin();
    if (viewIsLocalList) {
      this.state.dataRefresh.update(n => n + 1);
    }
  }
}
