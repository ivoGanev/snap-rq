import { Injectable, signal } from '@angular/core';

/** An actionable button rendered on a toast. */
export interface ToastAction {
  label: string;
  run: () => void | Promise<unknown>;
}

export interface ActiveToast {
  id: number;
  message: string;
  action: ToastAction | null;
}

/**
 * Minimal single-slot toast: showing a new toast replaces the current one
 * and restarts its timer. Used for brief undo/redo prompts.
 */
@Injectable({ providedIn: 'root' })
export class ToastService {
  private nextId = 0;
  private timer: number | undefined;

  readonly active = signal<ActiveToast | null>(null);

  show(message: string, action: ToastAction | null = null, durationMs = 5000): void {
    if (this.timer !== undefined) {
      window.clearTimeout(this.timer);
    }
    this.active.set({ id: ++this.nextId, message, action });
    if (durationMs > 0) {
      this.timer = window.setTimeout(() => this.dismiss(), durationMs);
    }
  }

  dismiss(): void {
    if (this.timer !== undefined) {
      window.clearTimeout(this.timer);
      this.timer = undefined;
    }
    this.active.set(null);
  }

  runAction(): void {
    const toast = this.active();
    this.dismiss();
    void toast?.action?.run();
  }
}
