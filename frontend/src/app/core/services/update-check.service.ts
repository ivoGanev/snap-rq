import { Injectable, signal } from '@angular/core';
import { UpdaterService, type UpdateInfo } from '../../../../bindings/snap-rq';

/** After startup, so the check doesn't compete with loading the workspace. */
const FIRST_CHECK_MS = 10_000;
const CHECK_EVERY_MS = 6 * 60 * 60 * 1000;

/**
 * Whether a newer SnapRQ is out: checked soon after startup and every six
 * hours, and by Settings → Updates. The top bar shows an icon while one is.
 */
@Injectable({ providedIn: 'root' })
export class UpdateCheckService {
  /** The newer release, or null when up to date or not checked yet. */
  readonly available = signal<UpdateInfo | null>(null);

  private started = false;

  async start(): Promise<void> {
    if (this.started) return;
    this.started = true;
    const version = await UpdaterService.CurrentVersion().catch(() => '');
    // Development and test builds always see the latest release as newer.
    if (!version || version === '0.0.0-dev') return;
    setTimeout(() => void this.checkQuietly(), FIRST_CHECK_MS);
    setInterval(() => void this.checkQuietly(), CHECK_EVERY_MS);
  }

  /** Asks GitHub for a newer release; errors reach the caller. */
  async check(): Promise<UpdateInfo | null> {
    const info = (await UpdaterService.Check()) ?? null;
    this.available.set(info);
    return info;
  }

  private async checkQuietly(): Promise<void> {
    try {
      await this.check();
    } catch {
      // Offline or rate-limited: the icon just doesn't show; Settings → Updates reports errors.
    }
  }
}
