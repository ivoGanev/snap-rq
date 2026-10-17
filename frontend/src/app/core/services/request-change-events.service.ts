import { Injectable, signal } from '@angular/core';

/**
 * Lightweight pub/sub for cross-component request change notifications.
 *
 * Used when one editor (e.g. the Zen-mode request editor) mutates a request
 * and other open UI surfaces (e.g. the body drawer) need to refresh.
 */
@Injectable({ providedIn: 'root' })
export class RequestChangeEventsService {
  readonly bodyChanged = signal<{ id: number; at: number } | null>(null);

  notifyBodyChanged(id: number): void {
    this.bodyChanged.set({ id, at: Date.now() });
  }
}
