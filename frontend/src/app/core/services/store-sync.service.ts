import { Injectable, inject } from '@angular/core';
import { Subject, takeUntil } from 'rxjs';
import { CommandApiService, type CommandChange } from './command.service';
import { WorkspaceStateService } from './workspace-state.service';
import { ApiRequestsService } from './api-requests.service';
import { EnvironmentApiService } from './environment.service';
import { EnvironmentVariableApiService } from './environment-variable.service';
import { FavouriteApiService } from './favourite.service';
import { RequestApiService } from './request.service';
import type { HttpRequestSummary } from '../models/http';

/**
 * Keeps the in-memory stores in step with committed commands, undos and
 * redos — with targeted refreshes, not full reloads.
 *
 * The backend's 'command:changed' event carries the command name, the
 * input it was invoked with, and the result it returned. Request ids are
 * extracted from whichever of those has them, then `request.list_by_ids`
 * fetches just the touched rows — ids that come back are upserted, ids
 * that vanished are dropped. This is what keeps undo snappy: one small
 * IPC call instead of a full `loadAll()` plus a view reload.
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
    const prefixes = new Set(changes.map(c => c.command.split('.')[0]));

    try {
      if (prefixes.has('request') || prefixes.has('response') || prefixes.has('bin')) {
        await this.refreshRequests(changes);
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
   * Extracts the request ids a change touched from the input or the
   * result, then refreshes just those rows. Covers every shape:
   *
   *   delete / bulk_set:  input has {ids: [...]}
   *   update / reorder:   input has {id} or {ordered_ids: [...]}
   *   create undo:        result has the assigned {id}
   *   duplicate undo:     result has the copy's {id}
   *   bin.restore undo:   result has [{id}, {id}, ...]
   *
   * Falls back to loadAll() only when nothing yields ids.
   */
  private async refreshRequests(changes: CommandChange[]): Promise<void> {
    const ids = new Set<number>();
    for (const change of changes) {
      extractIds(change.input, ids);
      extractIds(change.result, ids);
    }

    if (ids.size > 0) {
      // Targeted: fetch just the touched rows; ids that vanished are
      // dropped, ids that exist are upserted.
      const summaries = await this.requestApi.getSummariesByIds([...ids]);
      const present = new Set(summaries.map(s => s.id));
      for (const summary of summaries) {
        this.apiRequests.addOrReplace(summary);
        this.requestApi.addToRequests(summary);
      }
      const gone = [...ids].filter(id => !present.has(id));
      if (gone.length > 0) {
        this.apiRequests.removeMany(gone);
        this.requestApi.removeManyFromRequests(gone);
      }
    } else {
      // No ids extracted (a command shape we don't parse): reload all.
      await this.apiRequests.loadAll();
    }

    // View-local lists (favourite/tag/recent/bin) hold their own copies.
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

/**
 * Extracts request ids from a JSON string into the given set. Handles
 * every shape the request commands use: {ids: [...]}, {id}, {ordered_ids:
 * [...]}, and arrays of objects with {id}.
 */
function extractIds(raw: string, ids: Set<number>): void {
  if (!raw || raw === 'null' || raw === '""') return;
  let parsed: any;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return;
  }
  if (parsed == null) return;

  // {ids: [1, 2]} — delete, bulk_set
  if (Array.isArray(parsed.ids)) {
    for (const id of parsed.ids) ids.add(Number(id));
  }
  // {id: 42} — update, create result, duplicate result
  if (parsed.id != null && typeof parsed.id === 'number') {
    ids.add(parsed.id);
  }
  // {ordered_ids: [1, 2]} — reorder
  if (Array.isArray(parsed.ordered_ids)) {
    for (const id of parsed.ordered_ids) ids.add(Number(id));
  }
  // {request_ids: [1, 2]} — favourite remove
  if (Array.isArray(parsed.request_ids)) {
    for (const id of parsed.request_ids) ids.add(Number(id));
  }
  // [{id: 1}, {id: 2}] — bin.restore result (array of restored requests)
  if (Array.isArray(parsed)) {
    for (const item of parsed) {
      if (item?.id != null && typeof item.id === 'number') {
        ids.add(item.id);
      }
    }
  }
}
