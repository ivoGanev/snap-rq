import { Injectable, signal } from '@angular/core';
import { Events } from '@wailsio/runtime';
import { Observable, Subject } from 'rxjs';
import { filter } from 'rxjs/operators';
import type { CommandChanged, EntityChange } from '../models/change-set';

/**
 * The one event stream for data changes. It listens to the backend's
 * 'command:changed' (fired for execute, undo and redo alike), converts it
 * into a change set of affected row ids, and publishes it. Stores
 * subscribe to the component they own; components read stores.
 *
 * This class is a leaf — it injects nothing, so any service can depend on
 * it without creating a cycle.
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
// Command → change set. This is the single place that knows the shapes of
// command inputs/results, so stores stay dumb.
// --------------------------------------------------------------------------

// Components that own a store which reloads its own bounded list on any
// change (collections, projects, tags, …).
const metadataComponents = new Set([
  'bin',
  'favourite',
  'environment',
  'variable',
  'collection',
  'project',
  'tag',
]);

function toChanges(raw: CommandChanged): EntityChange[] {
  const changes: EntityChange[] = [];

  // request rows (also touched by response saves, bin restores and
  // collection deletes)
  const ids = requestIds(raw);
  if (ids.length > 0) {
    changes.push({ component: 'request', operation: raw.operation, ids });
  }

  if (metadataComponents.has(raw.component)) {
    changes.push({ component: raw.component, operation: raw.operation, ids: [] });
  }
  return changes;
}

/** The request ids a command touched — the exact rows to refetch. */
function requestIds(raw: CommandChanged): number[] {
  const ids = new Set<number>();
  const input = parse(raw.input);
  const result = parse(raw.result);

  switch (raw.component) {
    case 'request':
      collectField(input, 'ids', ids);
      collectField(input, 'id', ids);
      collectField(input, 'ordered_ids', ids);
      collectField(result, 'id', ids);
      collectArrayItems(result, 'id', ids);
      break;

    case 'response':
      if (isNumber(input?.request_id)) ids.add(input.request_id);
      if (Array.isArray(input?.responses)) {
        for (const r of input.responses) if (isNumber(r?.request_id)) ids.add(r.request_id);
      }
      if (Array.isArray(result)) {
        for (const r of result) if (isNumber(r?.request_id)) ids.add(r.request_id);
      }
      break;

    case 'bin':
      // bin.restore returns the recreated requests; bin trash row ids are
      // not request ids and must never be treated as such.
      collectArrayItems(result, 'id', ids);
      break;

    case 'collection':
      // collection.delete reports the requests that died with it
      collectField(result, 'request_ids', ids);
      break;
  }
  return [...ids];
}

function parse(raw: string): any {
  if (!raw) return null;
  try {
    return JSON.parse(raw);
  } catch {
    return null;
  }
}

function isNumber(value: unknown): value is number {
  return typeof value === 'number';
}

/** Reads a field that is either a single id or an array of ids. */
function collectField(source: any, field: string, ids: Set<number>): void {
  const value = source?.[field];
  if (isNumber(value)) ids.add(value);
  if (Array.isArray(value)) {
    for (const id of value) if (isNumber(id)) ids.add(id);
  }
}

/** Reads a field from each item of a top-level array. */
function collectArrayItems(source: any, field: string, ids: Set<number>): void {
  if (!Array.isArray(source)) return;
  for (const item of source) if (isNumber(item?.[field])) ids.add(item[field]);
}
