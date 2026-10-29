import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { CommandApiService, type StackEntryInfo } from '../core/services/command.service';

/**
 * Development-only inspector for the undo stack: lists every live entry and
 * lets you unwind step by step (or rewind to a position, which is strictly
 * sequential LIFO undo) to see what happens.
 */
@Component({
  selector: 'app-undo-panel',
  templateUrl: './undo-panel.html',
  styleUrl: './undo-panel.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class UndoPanel {
  protected readonly commands = inject(CommandApiService);

  protected readonly entries = signal<StackEntryInfo[]>([]);
  protected readonly loading = signal(false);
  protected readonly status = signal<string | null>(null);

  /** Newest first, so the next thing an Undo would hit is on top. */
  protected readonly ordered = computed(() => [...this.entries()].reverse());

  constructor() {
    void this.refresh();
  }

  async refresh(): Promise<void> {
    this.loading.set(true);
    try {
      this.entries.set(await this.commands.stack());
    } catch (err) {
      this.status.set(`Failed to load stack: ${String(err)}`);
    } finally {
      this.loading.set(false);
    }
  }

  async undoOnce(): Promise<void> {
    try {
      const result = await this.commands.undo();
      this.describe(result.notices?.length ? result.notices.join('; ') : `Undid ${result.label}`);
    } catch (err) {
      this.status.set(`Undo failed: ${String(err)}`);
    }
    await this.refresh();
  }

  async redoOnce(): Promise<void> {
    try {
      const result = await this.commands.redo();
      this.describe(`Redid ${result.label}`);
    } catch (err) {
      this.status.set(`Redo failed: ${String(err)}`);
    }
    await this.refresh();
  }

  /**
   * Rewinds until the given entry has been undone. Repeated single-step
   * undo keeps the LIFO discipline that makes unwinding referentially safe.
   */
  async rewindTo(target: StackEntryInfo): Promise<void> {
    this.loading.set(true);
    try {
      for (;;) {
        const live = this.entries().some(e => e.seq === target.seq && e.state === 'undo');
        if (!live) {
          break;
        }
        try {
          await this.commands.undo();
        } catch {
          break;
        }
        await this.syncEntries();
      }
      this.describe(`Rewound to before #${target.seq} ${target.label}`);
    } finally {
      this.loading.set(false);
      await this.refresh();
    }
  }

  close(): void {
    this.commands.closeDevPanel();
  }

  private async syncEntries(): Promise<void> {
    this.entries.set(await this.commands.stack());
  }

  private describe(message: string): void {
    this.status.set(message);
  }

  formatTables(entry: StackEntryInfo): string {
    return Object.entries(entry.tables ?? {})
      .map(([table, ids]) => `${table}: ${ids?.length ? ids.join(', ') : '—'}`)
      .join(' · ');
  }
}
