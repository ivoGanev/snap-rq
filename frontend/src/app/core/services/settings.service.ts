import { Injectable, inject, signal } from '@angular/core';
import { CommandApiService } from './command.service';

export interface AppSettings {
  validate_ssl_certificates: boolean;
  request_timeout_seconds: number;
  undo_history_limit: number;
}

/**
 * The app's typed API for settings.
 */
@Injectable({ providedIn: 'root' })
export class SettingsApiService {
  private readonly commands = inject(CommandApiService);

  readonly settings = signal<AppSettings>({
    validate_ssl_certificates: true,
    request_timeout_seconds: 30,
    undo_history_limit: 100,
  });
  readonly loaded = signal(false);

  async load(): Promise<void> {
    try {
      this.settings.set(await this.commands.execute<AppSettings>('settings.get', {}));
    } catch (err) {
      console.error('Failed to load settings:', err);
    } finally {
      this.loaded.set(true);
    }
  }

  async update(settings: AppSettings): Promise<AppSettings> {
    const saved = await this.commands.execute<AppSettings>('settings.update', settings);
    this.settings.set(saved);
    return saved;
  }
}