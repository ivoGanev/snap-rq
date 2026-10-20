import { Injectable, signal } from '@angular/core';
import * as Services from '../../../../bindings/snap-rq/backend/services';
import type { AppSettings } from '../../../../bindings/snap-rq/backend/models';

export type { AppSettings };

/**
 * Angular wrapper around the Wails-generated SettingsService bindings.
 */
@Injectable({ providedIn: 'root' })
export class SettingsApiService {
  readonly settings = signal<AppSettings>({ validate_ssl_certificates: true });
  readonly loaded = signal(false);

  async load(): Promise<void> {
    try {
      this.settings.set(await Services.SettingsService.GetSettings());
    } catch (err) {
      console.error('Failed to load settings:', err);
    } finally {
      this.loaded.set(true);
    }
  }

  async update(settings: AppSettings): Promise<AppSettings> {
    const saved = await Services.SettingsService.UpdateSettings(settings);
    this.settings.set(saved);
    return saved;
  }
}
