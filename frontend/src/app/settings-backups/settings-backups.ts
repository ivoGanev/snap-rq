import { Component, computed, inject, signal } from '@angular/core';
import { Dialogs } from '@wailsio/runtime';
import {
  BackupApiService,
  type BackupInfo,
  type BackupSettings,
  type BackupTrigger,
} from '../core/services/backup.service';
import { ConfirmService } from '../core/services/confirm.service';
import { SecretsApiService } from '../core/services/secrets.service';
import { RecoveryKeyDialog } from '../dialogs/recovery-key-dialog/recovery-key-dialog';

const TRIGGER_LABELS: Record<BackupTrigger, string> = {
  delete_project: 'Deleting a project',
  delete_collection: 'Deleting a collection with requests',
  delete_environment: 'Deleting an environment with variables',
  delete_requests: 'Deleting many requests at once',
  permanent_delete: 'Deleting from the Trash forever',
  openapi_import: 'Importing an OpenAPI spec',
  secrets_reset: 'Clearing secret values (Start over)',
  app_update: 'Updating SnapRQ',
};

/** A backup on its way to being restored. */
interface RestoreState {
  path: string;
  label: string;
  step: 'unlock' | 'working' | 'ready';
  error: string;
}

/**
 * Settings → Backups: when automatic backups happen, where they go, how many
 * are kept, an optional backup password, and the history with restore.
 */
@Component({
  selector: 'app-settings-backups',
  imports: [RecoveryKeyDialog],
  templateUrl: './settings-backups.html',
  styleUrl: './settings-backups.scss',
})
export class SettingsBackups {
  protected readonly backupApi = inject(BackupApiService);
  private readonly confirm = inject(ConfirmService);
  protected readonly secretsApi = inject(SecretsApiService);

  protected readonly settings = this.backupApi.settings;
  protected readonly backups = this.backupApi.backups;
  protected readonly labels = TRIGGER_LABELS;

  protected readonly busy = signal(false);
  protected readonly notice = signal('');
  protected readonly passwordOpen = signal(false);
  protected readonly passwordInput = signal('');
  protected readonly recoveryKeyOpen = signal(false);
  protected readonly restore = signal<RestoreState | null>(null);
  protected readonly unlockInput = signal('');

  /** Backups are encrypted with the secrets key: remind to save its recovery key. */
  protected readonly recoveryKeyUnseen = computed(() => this.secretsApi.status()?.recovery_key === 'unseen');

  constructor() {
    void this.backupApi.loadSettings();
    void this.backupApi.loadBackups();
    void this.secretsApi.refresh();
  }

  protected triggerOn(trigger: BackupTrigger): boolean {
    return this.settings()?.triggers[trigger] !== false;
  }

  protected async setEnabled(event: Event): Promise<void> {
    await this.save({ enabled: (event.target as HTMLInputElement).checked });
  }

  protected async setTrigger(trigger: BackupTrigger, event: Event): Promise<void> {
    const current = this.settings();
    if (!current) return;
    await this.save({ triggers: { ...current.triggers, [trigger]: (event.target as HTMLInputElement).checked } });
  }

  /** Number fields keep their limits; an empty or invalid entry snaps back. */
  protected async setNumber(
    field: 'request_delete_threshold' | 'keep_risky' | 'keep_updates',
    event: Event,
    max: number,
  ): Promise<void> {
    const input = event.target as HTMLInputElement;
    const current = this.settings();
    if (!current) return;
    const value = Math.trunc(Number(input.value));
    if (!Number.isFinite(value) || value < 1) {
      input.value = String(current[field]);
      return;
    }
    const clamped = Math.min(max, value);
    input.value = String(clamped);
    if (clamped !== current[field]) await this.save({ [field]: clamped });
  }

  protected async chooseFolder(): Promise<void> {
    const path = await Dialogs.OpenFile({
      Title: 'Choose the backup folder',
      CanChooseDirectories: true,
      CanChooseFiles: false,
      CanCreateDirectories: true,
    });
    if (typeof path === 'string' && path) {
      await this.save({ folder: path });
      await this.backupApi.loadBackups();
    }
  }

  protected async useDefaultFolder(): Promise<void> {
    await this.save({ folder: '' });
    await this.backupApi.loadBackups();
  }

  protected async savePassword(): Promise<void> {
    const current = this.settings();
    if (!current || !this.passwordInput()) return;
    await this.backupApi.updateSettings(current, this.passwordInput());
    this.passwordInput.set('');
    this.passwordOpen.set(false);
    this.notice.set('Backup password saved. New backups can also be opened with it.');
  }

  protected async removePassword(): Promise<void> {
    const current = this.settings();
    if (!current) return;
    await this.backupApi.updateSettings(current, '');
    this.notice.set('Backup password removed.');
  }

  protected async backUpNow(): Promise<void> {
    this.busy.set(true);
    this.notice.set('');
    try {
      await this.backupApi.backUpNow();
      this.notice.set('Backup saved.');
    } catch (err) {
      this.notice.set(`Backup failed: ${(err as Error).message ?? err}`);
    } finally {
      this.busy.set(false);
    }
  }

  protected async remove(info: BackupInfo): Promise<void> {
    const ok = await this.confirm.ask({
      title: 'Delete this backup?',
      message: `${this.describe(info)} will be deleted from the backup folder.`,
      confirmLabel: 'Delete backup',
    });
    if (ok) await this.backupApi.delete(info.path);
  }

  // ---------- Restore ----------

  protected async startRestore(info: BackupInfo): Promise<void> {
    await this.restoreFrom(info.path, this.describe(info));
  }

  protected async restoreFromFile(): Promise<void> {
    const path = await Dialogs.OpenFile({
      Title: 'Restore a backup',
      Filters: [{ DisplayName: 'SnapRQ backups', Pattern: '*.snaprq-backup' }],
    });
    if (typeof path === 'string' && path) await this.restoreFrom(path, path.split(/[\\/]/).pop() ?? path);
  }

  private async restoreFrom(path: string, label: string): Promise<void> {
    const ok = await this.confirm.ask({
      title: 'Restore this backup?',
      message: `SnapRQ goes back to ${label} and restarts. Your current data is backed up first, so this can be undone.`,
      confirmLabel: 'Restore and restart',
    });
    if (!ok) return;
    this.restore.set({ path, label, step: 'working', error: '' });
    await this.attemptRestore({});
  }

  protected async unlockAndRestore(): Promise<void> {
    const value = this.unlockInput().trim();
    if (!value) return;
    // A recovery key starts with SNRQ; anything else is the backup password.
    const unlock = /^snrq[\s-]/i.test(value) ? { recovery_key: value } : { password: value };
    this.restore.update((r) => (r ? { ...r, step: 'working', error: '' } : r));
    await this.attemptRestore(unlock);
  }

  private async attemptRestore(unlock: { recovery_key?: string; password?: string }): Promise<void> {
    const state = this.restore();
    if (!state) return;
    try {
      const result = await this.backupApi.restore(state.path, unlock);
      if (result.status === 'ready') {
        this.restore.set({ ...state, step: 'ready', error: '' });
        await this.backupApi.restart();
        return;
      }
      this.restore.set({
        ...state,
        step: 'unlock',
        error: result.status === 'wrong_key' ? "That recovery key or password doesn't open this backup." : '',
      });
    } catch (err) {
      this.restore.set(null);
      this.notice.set(`Restore failed: ${(err as Error).message ?? err}`);
    }
  }

  protected cancelRestore(): void {
    this.restore.set(null);
    this.unlockInput.set('');
  }

  // ---------- Helpers ----------

  protected describe(info: BackupInfo): string {
    return `the backup from ${new Date(info.header.created_at).toLocaleString()}`;
  }

  protected date(info: BackupInfo): string {
    return new Date(info.header.created_at).toLocaleString();
  }

  protected size(bytes: number): string {
    return bytes >= 1e6 ? `${(bytes / 1e6).toFixed(1)} MB` : `${Math.max(1, Math.round(bytes / 1e3))} KB`;
  }

  private async save(changes: Partial<BackupSettings>): Promise<void> {
    const current = this.settings();
    if (!current) return;
    try {
      await this.backupApi.updateSettings({ ...current, ...changes });
    } catch (err) {
      console.error('saving backup settings failed', err);
      await this.backupApi.loadSettings();
    }
  }
}
