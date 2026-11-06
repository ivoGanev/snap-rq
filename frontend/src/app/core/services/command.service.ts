import { Injectable, inject, signal } from '@angular/core';
import { Events } from '@wailsio/runtime';
import * as Commands from '../../../../bindings/snap-rq/backend/commands';
import type {
  CommandInfo,
  Status,
  StepInfo,
  UndoResult,
} from '../../../../bindings/snap-rq/backend/commands/models';
import { ToastService } from './toast.service';

export type { CommandInfo, Status, StepInfo, UndoResult };

/**
 * Angular wrapper around the Wails-generated CommandService bindings, plus
 * the undo menu state mirrored from 'command:history'. Data changes are
 * published on the ChangeBus, not here.
 */
@Injectable({ providedIn: 'root' })
export class CommandApiService {
  private readonly toasts = inject(ToastService);

  readonly undoSteps = signal(0);
  readonly redoSteps = signal(0);
  readonly nextUndoLabel = signal('');
  readonly nextRedoLabel = signal('');
  readonly devPanelOpen = signal(false);

  constructor() {
    Events.On('command:history', ev => this.onStatus(ev.data as Status));
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
    const result = await Commands.CommandService.Execute(name, JSON.stringify(payload));
    return result.result as T;
  }

  /** Starts an observable command and returns its run id. */
  async begin(name: string, payload: object): Promise<string> {
    return Commands.CommandService.Begin(name, JSON.stringify(payload));
  }

  async undo(): Promise<UndoResult> {
    return Commands.CommandService.Undo();
  }

  async redo(): Promise<UndoResult> {
    return Commands.CommandService.Redo();
  }

  async stack(): Promise<StepInfo[]> {
    const steps = await Commands.CommandService.Stack();
    return steps ?? [];
  }

  async listCommands(): Promise<CommandInfo[]> {
    const commands = await Commands.CommandService.ListCommands();
    return commands ?? [];
  }

  openDevPanel(): void {
    this.devPanelOpen.set(true);
  }

  closeDevPanel(): void {
    this.devPanelOpen.set(false);
  }
}
