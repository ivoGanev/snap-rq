import { Component, computed, DestroyRef, inject, OnInit, signal } from '@angular/core';
import { Browser, Events, Updater } from '@wailsio/runtime';
import { UpdaterService, type UpdateInfo } from '../../../bindings/snap-rq';
import { UpdateCheckService } from '../core/services/update-check.service';

type Phase = 'idle' | 'checking' | 'upToDate' | 'available' | 'blocked' | 'working' | 'ready' | 'error';

/**
 * Checks GitHub Releases for a newer SnapRQ and installs it, all inside the
 * settings window. Nothing is downloaded until the user asks.
 */
@Component({
  selector: 'app-settings-updates',
  templateUrl: './settings-updates.html',
  styleUrl: './settings-updates.scss',
})
export class SettingsUpdates implements OnInit {
  private readonly updates = inject(UpdateCheckService);
  protected readonly phase = signal<Phase>('idle');
  protected readonly currentVersion = signal('');
  protected readonly newVersion = signal('');
  protected readonly releaseUrl = signal('');
  protected readonly stage = signal('Downloading');
  protected readonly written = signal(0);
  protected readonly total = signal(0);
  protected readonly message = signal('');

  protected readonly percent = computed(() => {
    const total = this.total();
    return total > 0 ? Math.min(100, Math.round((this.written() / total) * 100)) : 0;
  });

  constructor() {
    const offs = [
      Events.On(Updater.Events.DownloadProgress, (event) => {
        const data = event.data as { written?: number; total?: number } | null;
        this.stage.set('Downloading');
        this.written.set(data?.written ?? 0);
        this.total.set(data?.total ?? 0);
      }),
      Events.On(Updater.Events.Verifying, () => this.stage.set('Verifying')),
      Events.On(Updater.Events.Installing, () => this.stage.set('Installing')),
    ];
    inject(DestroyRef).onDestroy(() => offs.forEach((off) => off()));
  }

  ngOnInit(): void {
    UpdaterService.CurrentVersion()
      .then((version) => this.currentVersion.set(version))
      .catch(() => undefined);
    // Opened from the top bar's update icon: the newer version is already known.
    const known = this.updates.available();
    if (known) this.show(known);
  }

  protected async check(): Promise<void> {
    this.phase.set('checking');
    this.message.set('');
    try {
      const info = await this.updates.check();
      if (info) {
        this.show(info);
      } else {
        this.phase.set('upToDate');
      }
    } catch (err) {
      this.fail(err);
    }
  }

  protected async install(): Promise<void> {
    this.phase.set('working');
    this.stage.set('Downloading');
    this.written.set(0);
    this.total.set(0);
    try {
      await UpdaterService.Install();
      this.phase.set('ready');
    } catch (err) {
      this.fail(err);
    }
  }

  protected async restart(): Promise<void> {
    try {
      await UpdaterService.Restart();
    } catch (err) {
      this.fail(err);
    }
  }

  private show(info: UpdateInfo): void {
    this.newVersion.set(info.version);
    this.releaseUrl.set(info.release_url);
    // This install can't replace itself: say why, instead of a button that would fail.
    if (info.blocked) {
      this.message.set(info.blocked);
      this.phase.set('blocked');
    } else {
      this.phase.set('available');
    }
  }

  protected openRelease(): void {
    void Browser.OpenURL(this.releaseUrl()).catch((err) => console.error(err));
  }

  private fail(err: unknown): void {
    this.message.set(err instanceof Error ? err.message : String(err));
    this.phase.set('error');
  }
}
