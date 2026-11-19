import { Injectable, signal } from '@angular/core';
import type { OpenApiPick, OpenApiPreviewCollection } from './openapi.service';

interface ActivePrompt {
  rows: OpenApiPreviewCollection[];
  resolve: (picks: OpenApiPick[] | null) => void;
}

/**
 * Opens the import dialog at the app root (so it is centred over the whole
 * window, not inside the sidebar) and resolves with what the user ticked, or
 * null when they cancel. The dialog itself is mounted once in the app shell.
 */
@Injectable({ providedIn: 'root' })
export class OpenApiImportPrompt {
  private readonly state = signal<ActivePrompt | null>(null);

  readonly active = this.state.asReadonly();

  choose(rows: OpenApiPreviewCollection[]): Promise<OpenApiPick[] | null> {
    this.state()?.resolve(null);
    return new Promise((resolve) => this.state.set({ rows, resolve }));
  }

  respond(picks: OpenApiPick[] | null): void {
    const active = this.state();
    this.state.set(null);
    active?.resolve(picks);
  }
}
