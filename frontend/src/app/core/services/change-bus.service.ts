import { Injectable, signal } from '@angular/core';
import { Events } from '@wailsio/runtime';
import { Observable, Subject } from 'rxjs';
import { filter } from 'rxjs/operators';
import type { CommandChanged, EntityChange } from '../models/change-set';

/**
 * The one event stream for data changes. It listens to the backend's
 * 'command:changed' (fired for execute, undo and redo alike), converts it
 * into a change set of changed row ids, and publishes it. Stores
 * subscribe to the component they own; components read stores.
 */
@Injectable({ providedIn: 'root' })
export class ChangeBus {
  private readonly source = new Subject<EntityChange>();

  /** Bumped after any published change, for views holding their own lists. */
  readonly revision = signal(0);

  constructor() {
    Events.On('command:changed', ev => this.publish(toChanges(ev.data as CommandChanged)));
  }

  publish(changes: EntityChange[]): void {
    if (changes.length === 0) return;
    for (const change of changes) {
      this.source.next(change);
    }
    this.revision.update(n => n + 1);
  }

  /** Typed per-component stream — a store only sees what it owns. */
  changesFor(component: string): Observable<EntityChange> {
    return this.source.pipe(filter(change => change.component === component));
  }
}

// --------------------------------------------------------------------------
// Command → change set. The backend already says which rows it touched; the
// bus only turns each kind into one routed change.
// --------------------------------------------------------------------------

function toChanges(raw: CommandChanged): EntityChange[] {
  return (raw.changes ?? []).map(change => ({
    component: change.component,
    operation: raw.operation,
    ids: change.ids ?? [],
  }));
}
