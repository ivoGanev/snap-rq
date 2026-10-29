import { Injectable, inject, signal } from '@angular/core';
import { Events } from '@wailsio/runtime';
import { Subject } from 'rxjs';
import * as CommandService from '../../../../bindings/snap-rq/backend/services';
import type {
  CommandInfo,
  HistorySnapshot,
  StackEntryInfo,
  UndoResult,
} from '../../../../bindings/snap-rq/backend/commands/models';
import { ToastService } from './toast.service';

export type { CommandInfo, HistorySnapshot, StackEntryInfo, UndoResult };

/** Payload of the backend 'command:changed' event: IDs only, never values. */
export interface CommandChange {
  entity: string;
  op: string;
  ids: number[];
}

/**
 * Angular wrapper around the Wails-generated CommandService bindings, plus
 * the live history state mirrored from backend events. The backend owns the
 * stacks; these signals only reflect the last 'command:history' event.
 */
@Injectable({ providedIn: 'root' })
export class CommandApiService {
  private readonly toasts = inject(ToastService);

  readonly undoDepth = signal(0);
  readonly redoDepth = signal(0);
  readonly nextUndoLabel = signal('');
  readonly nextRedoLabel = signal('');
  readonly devPanelOpen = signal(false);

  /** Entity deltas from every committed command (and undo/redo replays). */
  private readonly changes = new Subject<CommandChange[]>();
  readonly change$ = this.changes.asObservable();

  constructor() {
    Events.On('command:history', ev => this.onHistory(ev.data as HistorySnapshot));
    Events.On('command:changed', ev => {
      const payload = ev.data as { command: string; changes: CommandChange[] | null };
      this.changes.next(payload.changes ?? []);
    });
  }

  private onHistory(history: HistorySnapshot): void {
    this.undoDepth.set(history.undoDepth);
    this.redoDepth.set(history.redoDepth);
    this.nextUndoLabel.set(history.nextUndoLabel);
    this.nextRedoLabel.set(history.nextRedoLabel);

    // Toasts: brief, actionable, single slot. Executes offer Undo, undos
    // offer Redo, redos offer Undo.
    switch (history.lastAction) {
      case 'execute':
        if (history.lastLabel) {
          this.toasts.show(history.lastLabel, { label: 'Undo', run: () => this.undo() });
        }
        break;
      case 'undo':
        this.toasts.show(`Undid ${history.lastLabel}`, { label: 'Redo', run: () => this.redo() });
        break;
      case 'redo':
        this.toasts.show(`Redid ${history.lastLabel}`, { label: 'Undo', run: () => this.undo() });
        break;
    }
  }

  /** Executes a named command with a JSON-friendly payload. */
  async execute<T>(name: string, payload: object): Promise<T> {
    const result = await CommandService.CommandService.Execute(name, JSON.stringify(payload));
    return result.result as T;
  }

  async undo(): Promise<UndoResult> {
    return CommandService.CommandService.Undo();
  }

  async redo(): Promise<UndoResult> {
    return CommandService.CommandService.Redo();
  }

  async stack(): Promise<StackEntryInfo[]> {
    const entries = await CommandService.CommandService.Stack();
    return entries ?? [];
  }

  async listCommands(): Promise<CommandInfo[]> {
    const commands = await CommandService.CommandService.ListCommands();
    return commands ?? [];
  }

  openDevPanel(): void {
    this.devPanelOpen.set(true);
  }

  closeDevPanel(): void {
    this.devPanelOpen.set(false);
  }
}
