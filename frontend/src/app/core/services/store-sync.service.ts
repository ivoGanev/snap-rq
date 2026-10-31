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
 * emits 'command:changed' with the command name; this service routes the
 * refresh by the command's entity prefix (request.*, favourite.*, etc.).
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
    if (this.flushing) return;
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
    // collect the entity prefixes (the part before the first dot)
    const prefixes = new Set(changes.map(c => c.command.split('.')[0]));

    try {
      if (prefixes.has('request') || prefixes.has('response') || prefixes.has('bin')) {
        await this.refreshRequests();
      }
      if (prefixes.has('favourite')) {
        const project = this.state.selectedProject();
        if (project) {
          await this.favouriteApi.loadCollectionsForProject(project.id);
        }
        this.state.dataRefresh.update(n => n + 1);
      }
      if (prefixes.has('environment')) {
        const project = this.state.selectedProject();
        if (project) {
          await this.environmentApi.loadForProject(project.id);
          if (!this.state.selectedEnvironment()) {
            this.state.selectedEnvironment.set(this.environmentApi.environments()[0] ?? null);
          }
        }
      }
      if (prefixes.has('variable')) {
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
   * Request stores refresh by reloading the active view's data — the same
   * thing a view switch does, without clearing selection.
   */
  private async refreshRequests(): Promise<void> {
    await this.apiRequests.loadAll();

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
