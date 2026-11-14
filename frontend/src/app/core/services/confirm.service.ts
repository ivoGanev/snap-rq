import { Injectable, signal } from '@angular/core';

export interface ConfirmOptions {
  title: string;
  message: string;
  confirmLabel?: string;
  cancelLabel?: string;
  /** When false the confirm button uses the neutral style; defaults to danger. */
  danger?: boolean;
}

interface ActiveConfirm extends ConfirmOptions {
  resolve: (value: boolean) => void;
}

/**
 * Single-slot, app-wide confirmation dialog controller. Any component can await
 * `ask(...)`; the dialog is rendered once by ConfirmOutlet at the app shell.
 * Only one confirmation can be open at a time.
 */
@Injectable({ providedIn: 'root' })
export class ConfirmService {
  private readonly state = signal<ActiveConfirm | null>(null);

  readonly active = this.state.asReadonly();

  ask(options: ConfirmOptions): Promise<boolean> {
    // A new prompt supersedes any open one (that one counts as cancelled).
    this.state()?.resolve(false);
    return new Promise<boolean>((resolve) => {
      this.state.set({ ...options, resolve });
    });
  }

  respond(confirmed: boolean): void {
    const current = this.state();
    if (!current) return;
    this.state.set(null);
    current.resolve(confirmed);
  }
}
