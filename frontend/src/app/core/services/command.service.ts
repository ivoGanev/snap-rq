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

  /**
   * Runs an observable command: progress goes to onUpdate, and the promise
   * resolves with its result when it finishes (rejects with its error).
   * Events can arrive before Begin returns the run id (they travel on
   * another channel), so early events are kept until it does.
   */
  run<T>(
    name: string,
    payload: object,
    onUpdate?: (data: unknown) => void,
    signal?: AbortSignal,
  ): Promise<T> {
    return new Promise<T>((resolve, reject) => {
      type RunEvent =
        | { kind: 'update'; runId: string; data: unknown }
        | { kind: 'finish'; runId: string; result?: unknown; error?: string };
      let runId: string | null = null;
      const early: RunEvent[] = [];

      const deliver = (e: RunEvent): void => {
        if (e.runId !== runId) return;
        if (e.kind === 'update') {
          onUpdate?.(e.data);
          return;
        }
        stop();
        if (e.error) reject(new Error(e.error));
        else resolve(e.result as T);
      };
      const receive = (e: RunEvent): void => {
        if (runId === null) early.push(e);
        else deliver(e);
      };

      // A cancelled run still ends with its finish event (carrying an error).
      const cancel = (): void => {
        if (runId !== null) void Commands.CommandService.Cancel(runId);
      };
      signal?.addEventListener('abort', cancel, { once: true });

      const offUpdate = Events.On('command:update', (ev) => receive({ kind: 'update', ...ev.data }));
      const offFinish = Events.On('command:finish', (ev) => receive({ kind: 'finish', ...ev.data }));
      const stop = (): void => {
        offUpdate();
        offFinish();
        signal?.removeEventListener('abort', cancel);
      };

      Commands.CommandService.Begin(name, JSON.stringify(payload)).then(
        (id) => {
          runId = id;
          if (signal?.aborted) cancel();
          early.splice(0).forEach(deliver);
        },
        (err) => {
          stop();
          reject(err);
        },
      );
    });
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
