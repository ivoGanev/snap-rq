import { Injectable, inject, signal } from '@angular/core';
import { Events } from '@wailsio/runtime';
import { Subject } from 'rxjs';
import * as CommandService from '../../../../bindings/snap-rq/backend/services';
import type {
  CommandInfo,
  Status,
  StepInfo,
  UndoResult,
} from '../../../../bindings/snap-rq/backend/commands/models';
import { ToastService } from './toast.service';

export type { CommandInfo, Status, StepInfo, UndoResult };

/** Payload of the backend 'command:changed' event: ids only, never values. */
export interface CommandChange {
  entity: string;
  op: string;
  ids: number[];
}

/**
 * Angular wrapper around the Wails-generated CommandService bindings, plus
 * the undo menu state mirrored from backend events. The backend owns the
 * undo list; these signals only reflect the last 'command:history' event.
 */
@Injectable({ providedIn: 'root' })
export class CommandApiService {
  private readonly toasts = inject(ToastService);

  readonly undoSteps = signal(0);
  readonly redoSteps = signal(0);
  readonly nextUndoLabel = signal('');
  readonly nextRedoLabel = signal('');
  readonly devPanelOpen = signal(false);

  /** Entity deltas from every run command (and undo/redo replays). */
  private readonly changes = new Subject<CommandChange[]>();
  readonly change$ = this.changes.asObservable();

  constructor() {
    Events.On('command:history', ev => this.onStatus(ev.data as Status));
    Events.On('command:changed', ev => {
      const payload = ev.data as { command: string; changes: CommandChange[] | null };
      this.changes.next(payload.changes ?? []);
    });
  }

  private onStatus(status: Status): void {
    this.undoSteps.set(status.undoSteps);
    this.redoSteps.set(status.redoSteps);
    this.nextUndoLabel.set(status.nextUndoLabel);
    this.nextRedoLabel.set(status.nextRedoLabel);

    // Toasts: brief, actionable, one at a time. Runs offer Undo, undos
    // offer Redo, redos offer Undo.
    switch (status.lastAction) {
      case 'run':
        if (status.lastLabel) {
          this.toasts.show(status.lastLabel, { label: 'Undo', run: () => this.undo() });
        }
        break;
      case 'undo':
        this.toasts.show(`Undid ${status.lastLabel}`, { label: 'Redo', run: () => this.redo() });
        break;
      case 'redo':
        this.toasts.show(`Redid ${status.lastLabel}`, { label: 'Undo', run: () => this.undo() });
        break;
    }
  }

  /** Runs a named command with a JSON-friendly payload. */
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

  async stack(): Promise<StepInfo[]> {
    const steps = await CommandService.CommandService.Stack();
    return steps ?? [];
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
