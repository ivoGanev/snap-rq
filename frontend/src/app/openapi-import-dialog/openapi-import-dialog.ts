import { Component, OnInit, input, output, signal, computed } from '@angular/core';
import type {
  OpenApiPick,
  OpenApiPreviewCollection,
  OpenApiPreviewRequest,
} from '../core/services/openapi.service';
import { lineDiff } from './line-diff';

/**
 * The import dialog: for each collection in the file, what is new, what
 * differs from the request already in the project, and what is unchanged.
 * New requests start ticked; changed ones start unticked.
 */
@Component({
  selector: 'app-openapi-import-dialog',
  templateUrl: './openapi-import-dialog.html',
  host: { '(document:keydown.escape)': 'cancelled.emit()' },
  styleUrl: './openapi-import-dialog.scss',
})
export class OpenApiImportDialog implements OnInit {
  readonly rows = input.required<OpenApiPreviewCollection[]>();
  readonly confirmed = output<OpenApiPick[]>();
  readonly cancelled = output<void>();

  protected readonly lineDiff = lineDiff;

  private readonly ticked = signal<ReadonlySet<string>>(new Set());
  private readonly expanded = signal<ReadonlySet<string>>(new Set());

  protected readonly tickedNew = computed(() => this.countTicked('new'));
  protected readonly tickedChanged = computed(() => this.countTicked('changed'));
  protected readonly hasAnythingToDo = computed(() =>
    this.rows().some((col) => col.new + col.changed > 0),
  );

  ngOnInit(): void {
    const start = new Set<string>();
    for (const col of this.rows()) {
      for (const req of col.requests) {
        if (req.status === 'new') start.add(this.id(col, req));
      }
    }
    this.ticked.set(start);
  }

  protected newOf(col: OpenApiPreviewCollection): OpenApiPreviewRequest[] {
    return col.requests.filter((r) => r.status === 'new');
  }

  protected changedOf(col: OpenApiPreviewCollection): OpenApiPreviewRequest[] {
    return col.requests.filter((r) => r.status === 'changed');
  }

  protected unchangedOf(col: OpenApiPreviewCollection): OpenApiPreviewRequest[] {
    return col.requests.filter((r) => r.status === 'unchanged');
  }

  protected isTicked(col: OpenApiPreviewCollection, req: OpenApiPreviewRequest): boolean {
    return this.ticked().has(this.id(col, req));
  }

  protected toggle(col: OpenApiPreviewCollection, req: OpenApiPreviewRequest): void {
    const next = new Set(this.ticked());
    const id = this.id(col, req);
    if (!next.delete(id)) next.add(id);
    this.ticked.set(next);
  }

  /** 'all' | 'some' | 'none' of the collection's new and changed requests. */
  protected stateOf(col: OpenApiPreviewCollection): 'all' | 'some' | 'none' {
    const pickable = this.pickable(col);
    const on = pickable.filter((r) => this.isTicked(col, r)).length;
    if (on === 0) return 'none';
    return on === pickable.length ? 'all' : 'some';
  }

  protected toggleCollection(col: OpenApiPreviewCollection): void {
    const turnOn = this.stateOf(col) !== 'all';
    const next = new Set(this.ticked());
    for (const req of this.pickable(col)) {
      if (turnOn) next.add(this.id(col, req));
      else next.delete(this.id(col, req));
    }
    this.ticked.set(next);
  }

  protected setAll(on: boolean): void {
    const next = new Set<string>();
    if (on) {
      for (const col of this.rows()) {
        for (const req of this.pickable(col)) next.add(this.id(col, req));
      }
    }
    this.ticked.set(next);
  }

  protected isOpen(col: OpenApiPreviewCollection, req: OpenApiPreviewRequest | null): boolean {
    return this.expanded().has(this.openId(col, req));
  }

  protected toggleOpen(col: OpenApiPreviewCollection, req: OpenApiPreviewRequest | null): void {
    const next = new Set(this.expanded());
    const id = this.openId(col, req);
    if (!next.delete(id)) next.add(id);
    this.expanded.set(next);
  }

  protected confirm(): void {
    const picks: OpenApiPick[] = [];
    for (const col of this.rows()) {
      const add = this.newOf(col).filter((r) => this.isTicked(col, r)).map((r) => r.key);
      const update = this.changedOf(col).filter((r) => this.isTicked(col, r)).map((r) => r.key);
      if (add.length + update.length > 0) picks.push({ name: col.name, add, update });
    }
    if (picks.length > 0) this.confirmed.emit(picks);
  }

  protected importLabel(): string {
    const parts: string[] = [];
    if (this.tickedNew() > 0) parts.push(`add ${this.tickedNew()}`);
    if (this.tickedChanged() > 0) parts.push(`update ${this.tickedChanged()}`);
    return parts.length > 0 ? `Import (${parts.join(', ')})` : 'Import';
  }

  private pickable(col: OpenApiPreviewCollection): OpenApiPreviewRequest[] {
    return col.requests.filter((r) => r.status !== 'unchanged');
  }

  private countTicked(status: 'new' | 'changed'): number {
    let n = 0;
    for (const col of this.rows()) {
      for (const req of col.requests) {
        if (req.status === status && this.isTicked(col, req)) n++;
      }
    }
    return n;
  }

  private id(col: OpenApiPreviewCollection, req: OpenApiPreviewRequest): string {
    return `${col.name}\n${req.key}`;
  }

  private openId(col: OpenApiPreviewCollection, req: OpenApiPreviewRequest | null): string {
    return req ? this.id(col, req) : `${col.name}\n#unchanged`;
  }
}
