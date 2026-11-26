import { Injectable, signal } from '@angular/core';
import { Events } from '@wailsio/runtime';
import { UpdaterService } from '../../../../bindings/snap-rq';
import type { Release } from '../../../../bindings/snap-rq/backend/selfupdate';

/**
 * Whether a newer SnapRQ is out. The backend checks (soon after startup and
 * every six hours, and when Settings asks) and announces each change; the
 * top bar shows an icon while one is.
 */
@Injectable({ providedIn: 'root' })
export class UpdateCheckService {
  /** The newer release, or null when up to date or not checked yet. */
  readonly available = signal<Release | null>(null);

  constructor() {
    Events.On('update:available', (event) => this.available.set(event.data ?? null));
    // A window opened or reloaded after a check asks for what was found.
    UpdaterService.Available()
      .then((release) => this.available.set(release ?? null))
      .catch(() => undefined);
  }

  /** Checks now; errors reach the caller. */
  async check(): Promise<Release | null> {
    const release = (await UpdaterService.Check()) ?? null;
    this.available.set(release);
    return release;
  }
}
