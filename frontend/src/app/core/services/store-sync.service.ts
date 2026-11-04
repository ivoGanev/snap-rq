import { Injectable, inject } from '@angular/core';
import { CommandApiService, type CommandChange } from './command.service';
import { RequestApiService } from './request.service';
import { FavouriteApiService } from './favourite.service';
import { EnvironmentApiService } from './environment.service';
import { EnvironmentVariableApiService } from './environment-variable.service';
import { WorkspaceStateService } from './workspace-state.service';

/**
 * Routes command changes to the entity service that owns the affected
 * store. The entity services expose their load methods; this class knows
 * the workspace context and calls the right one — no service knows about
 * the workspace except this router.
 */
@Injectable({ providedIn: 'root' })
export class StoreSyncService {
  private readonly commands = inject(CommandApiService);
  private readonly requestApi = inject(RequestApiService);
  private readonly favouriteApi = inject(FavouriteApiService);
  private readonly environmentApi = inject(EnvironmentApiService);
  private readonly envVarApi = inject(EnvironmentVariableApiService);
  private readonly state = inject(WorkspaceStateService);

  constructor() {
    this.commands.change$.subscribe(changes => {
      void this.apply(changes);
    });
  }

  private async apply(changes: CommandChange[]): Promise<void> {
    for (const change of changes) {
      try {
        await this.route(change);
      } catch (err) {
        console.error('syncing stores after command change failed', err);
      }
    }
  }

  private async route(change: CommandChange): Promise<void> {
    const prefix = change.command.split('.')[0];
    const project = this.state.selectedProject();

    switch (prefix) {
      case 'request':
      case 'response':
        await this.requestApi.sync(change);
        this.bumpViewRefresh();
        break;

      case 'bin':
        await this.requestApi.sync(change);
        this.bumpViewRefresh();
        break;

      case 'favourite':
        if (project) {
          await this.favouriteApi.loadCollectionsForProject(project.id);
        }
        this.state.dataRefresh.update(n => n + 1);
        break;

      case 'environment':
        if (project) {
          await this.environmentApi.loadForProject(project.id);
          // The selected environment might have been deleted (or restored
          // by undo) — reselect if it's no longer in the list.
          const selected = this.state.selectedEnvironment();
          const stillExists = this.environmentApi.environments().some(e => e.id === selected?.id);
          if (!selected || !stillExists) {
            this.state.selectedEnvironment.set(this.environmentApi.environments()[0] ?? null);
          }
        }
        break;

      case 'variable': {
        const env = this.state.selectedEnvironment();
        if (env) {
          await this.envVarApi.loadForEnvironment(env.id);
        }
        break;
      }
    }
  }

  /** View-local lists (favourite/tag/recent/bin) hold their own copies. */
  private bumpViewRefresh(): void {
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
