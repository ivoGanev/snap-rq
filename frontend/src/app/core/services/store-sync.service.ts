import { Injectable, inject } from '@angular/core';
import { Subject, takeUntil } from 'rxjs';
import { CommandApiService, type CommandChange } from './command.service';
import { RequestApiService } from './request.service';
import { FavouriteApiService } from './favourite.service';
import { EnvironmentApiService } from './environment.service';
import { EnvironmentVariableApiService } from './environment-variable.service';

/**
 * Routes command changes to the entity service that owns the affected
 * store. Each service's sync() knows its own command shapes and refreshes
 * its own signals — this class is only the routing table.
 */
@Injectable({ providedIn: 'root' })
export class StoreSyncService {
  private readonly commands = inject(CommandApiService);
  private readonly requestApi = inject(RequestApiService);
  private readonly favouriteApi = inject(FavouriteApiService);
  private readonly environmentApi = inject(EnvironmentApiService);
  private readonly envVarApi = inject(EnvironmentVariableApiService);

  /** Entity prefix → the service that syncs it. */
  private readonly routes: Map<string, (c: CommandChange) => Promise<void>>;

  private pending: CommandChange[] = [];
  private flushing = false;
  private readonly destroy = new Subject<void>();

  constructor() {
    this.routes = new Map([
      ['request',      c => this.requestApi.sync(c)],
      ['response',     c => this.requestApi.sync(c)],
      ['bin',          c => this.requestApi.sync(c)],
      ['favourite',    c => this.favouriteApi.sync(c)],
      ['environment',  c => this.environmentApi.sync(c)],
      ['variable',     c => this.envVarApi.sync(c)],
    ]);

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
    for (const change of changes) {
      const sync = this.routes.get(change.command.split('.')[0]);
      if (sync) {
        try {
          await sync(change);
        } catch (err) {
          console.error('syncing stores after command change failed', err);
        }
      }
    }
  }
}
