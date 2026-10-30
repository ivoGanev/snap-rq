import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { CommandApiService, type StepInfo } from '../core/services/command.service';

/**
 * Development-only inspector for the undo list: shows every remembered
 * step and lets you unwind step by step (or rewind to a position, which is
 * just repeated single-step undo) to see what happens.
 */
@Component({
  selector: 'app-undo-panel',
  templateUrl: './undo-panel.html',
  styleUrl: './undo-panel.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class UndoPanel {
  protected readonly commands = inject(CommandApiService);

  protected readonly steps = signal<StepInfo[]>([]);
  protected readonly loading = signal(false);
  protected readonly status = signal<string | null>(null);

  /** Newest first, so the step an Undo would take next is on top. */
  protected readonly ordered = computed(() => [...this.steps()].reverse());

  constructor() {
    void this.refresh();
  }

  async refresh(): Promise<void> {
    this.loading.set(true);
    try {
      this.steps.set(await this.commands.stack());
    } catch (err) {
      this.status.set(`Failed to load the undo list: ${String(err)}`);
    } finally {
      this.loading.set(false);
    }
  }

  async undoOnce(): Promise<void> {
    try {
      const result = await this.commands.undo();
      this.report(result.skipped?.length ? result.skipped.join('; ') : `Undid ${result.label}`);
    } catch (err) {
      this.status.set(`Undo failed: ${String(err)}`);
    }
    await this.refresh();
  }

  async redoOnce(): Promise<void> {
    try {
      const result = await this.commands.redo();
      this.report(`Redid ${result.label}`);
    } catch (err) {
      this.status.set(`Redo failed: ${String(err)}`);
    }
    await this.refresh();
  }

  /**
   * Rewinds until the given step has been undone. Repeated single-step
   * undo keeps the back-to-front order that makes unwinding safe.
   */
  async rewindTo(target: StepInfo): Promise<void> {
    this.loading.set(true);
    try {
      for (;;) {
        const stillListed = this.steps().some(s => s.seq === target.seq && s.state === 'undo');
        if (!stillListed) {
          break;
        }
        try {
          await this.commands.undo();
        } catch {
          break;
        }
        this.steps.set(await this.commands.stack());
      }
      this.report(`Rewound to before #${target.seq} ${target.label}`);
    } finally {
      this.loading.set(false);
      await this.refresh();
    }
  }

  close(): void {
    this.commands.closeDevPanel();
  }

  private report(message: string): void {
    this.status.set(message);
  }

  formatTables(step: StepInfo): string {
    return Object.entries(step.tables ?? {})
      .map(([table, ids]) => `${table}: ${ids?.length ? ids.join(', ') : '—'}`)
      .join(' · ');
  }
}
