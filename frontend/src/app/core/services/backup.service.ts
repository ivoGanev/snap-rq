import { Injectable, inject, signal } from '@angular/core';
import { Events } from '@wailsio/runtime';
import { AppService } from '../../../../bindings/snap-rq';
import { CommandApiService } from './command.service';
import { ToastService } from './toast.service';

/** An operation that takes a backup before it runs; see backend/backup/settings.go. */
export type BackupTrigger =
  | 'delete_project'
  | 'delete_collection'
  | 'delete_environment'
  | 'delete_requests'
  | 'permanent_delete'
  | 'openapi_import'
  | 'secrets_reset'
  | 'app_update';

export interface BackupSettings {
  enabled: boolean;
  /** Missing means on. */
  triggers: Partial<Record<BackupTrigger, boolean>>;
  request_delete_threshold: number;
  folder: string;
  /** Backups taken before risky changes that are kept (default 10). */
  keep_risky: number;
  /** Backups taken before updates that are kept, counted separately (default 5). */
  keep_updates: number;
  has_password: boolean;
  default_folder: string;
  all_triggers: BackupTrigger[];
}

export interface BackupHeader {
  created_at: string;
  /** automatic: before a risky change · update: before an update · manual: never deleted automatically. */
  kind: 'automatic' | 'update' | 'manual';
  reason: string;
  app_version: string;
  database_bytes: number;
  key_fingerprint: string;
}

export interface BackupInfo {
  path: string;
  name: string;
  bytes: number;
  header: BackupHeader;
}

/** ready: restart to finish · needs_key / wrong_key: ask for the recovery key or backup password. */
export interface RestoreResult {
  status: 'ready' | 'needs_key' | 'wrong_key';
  backup: BackupHeader;
}

/**
 * Backups: settings, history, "Back up now" and restore. See
 * docs/plans/backups.md. Also turns the backend's backup:saved event into a
 * toast whose "Show" opens Settings → Backups.
 */
@Injectable({ providedIn: 'root' })
export class BackupApiService {
  private readonly commands = inject(CommandApiService);
  private readonly toasts = inject(ToastService);

  readonly settings = signal<BackupSettings | null>(null);
  readonly backups = signal<BackupInfo[]>([]);
  /** Bumped when a toast's "Show" asks for Settings → Backups. */
  readonly showRequested = signal(0);

  constructor() {
    Events.On('backup:saved', (ev) => {
      const { reason } = ev.data as { reason: string };
      this.toasts.show(`Backup saved ${reason}`, {
        label: 'Show',
        run: () => this.showRequested.update((n) => n + 1),
      });
      void this.loadBackups();
    });
  }

  async loadSettings(): Promise<BackupSettings> {
    const settings = await this.commands.execute<BackupSettings>('backup.settings', {});
    this.settings.set(settings);
    return settings;
  }

  /** Saves the settings; `password` is left alone when undefined, removed with "". */
  async updateSettings(settings: BackupSettings, password?: string): Promise<BackupSettings> {
    const saved = await this.commands.execute<BackupSettings>('backup.update_settings', {
      ...settings,
      ...(password === undefined ? {} : { password }),
    });
    this.settings.set(saved);
    return saved;
  }

  async loadBackups(): Promise<BackupInfo[]> {
    const list = await this.commands.execute<BackupInfo[]>('backup.list', {});
    this.backups.set(list ?? []);
    return list ?? [];
  }

  async backUpNow(): Promise<BackupInfo> {
    const info = await this.commands.run<BackupInfo>('backup.create', {});
    await this.loadBackups();
    return info;
  }

  async delete(path: string): Promise<void> {
    await this.commands.execute('backup.delete', { path });
    await this.loadBackups();
  }

  /** Gets a backup ready to restore; on "ready", call restart(). */
  async restore(path: string, unlock: { recovery_key?: string; password?: string } = {}): Promise<RestoreResult> {
    return this.commands.run<RestoreResult>('backup.restore', { path, ...unlock });
  }

  async restart(): Promise<void> {
    await AppService.Restart();
  }

  async showInFolder(path: string): Promise<void> {
    await AppService.ShowInFolder(path);
  }
}
