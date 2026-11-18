import { Component, inject } from '@angular/core';
import { SettingsApiService } from '../core/services/settings.service';

/** The everyday request settings: SSL, cookies, timeout and undo history. */
@Component({
  selector: 'app-settings-general',
  templateUrl: './settings-general.html',
})
export class SettingsGeneral {
  protected readonly settingsApi = inject(SettingsApiService);

  protected async onSslValidationChange(event: Event): Promise<void> {
    const checked = (event.target as HTMLInputElement).checked;
    try {
      await this.settingsApi.update({
        ...this.settingsApi.settings(),
        validate_ssl_certificates: checked,
      });
    } catch (err) {
      console.error('Failed to update settings:', err);
      this.settingsApi.settings.update((current) => ({ ...current }));
    }
  }

  protected async onCookieJarChange(event: Event): Promise<void> {
    const checked = (event.target as HTMLInputElement).checked;
    try {
      await this.settingsApi.update({ ...this.settingsApi.settings(), use_cookie_jar: checked });
    } catch (err) {
      console.error('Failed to update settings:', err);
      this.settingsApi.settings.update((current) => ({ ...current }));
    }
  }

  protected async onRequestTimeoutChange(event: Event): Promise<void> {
    const input = event.target as HTMLInputElement;
    const parsed = Math.trunc(Number(input.value));
    const value = Number.isFinite(parsed) ? Math.min(99999, Math.max(1, parsed)) : NaN;
    const current = this.settingsApi.settings();
    if (Number.isNaN(value) || input.value.trim() === '') {
      input.value = String(current.request_timeout_seconds);
      return;
    }
    input.value = String(value);
    if (value === current.request_timeout_seconds) {
      return;
    }
    try {
      await this.settingsApi.update({ ...current, request_timeout_seconds: value });
    } catch (err) {
      console.error('Failed to update settings:', err);
      input.value = String(current.request_timeout_seconds);
    }
  }

  protected async onUndoLimitChange(event: Event): Promise<void> {
    const input = event.target as HTMLInputElement;
    const parsed = Math.trunc(Number(input.value));
    const value = Number.isFinite(parsed) ? Math.min(1000, Math.max(10, parsed)) : NaN;
    const current = this.settingsApi.settings();
    if (Number.isNaN(value) || input.value.trim() === '') {
      input.value = String(current.undo_history_limit);
      return;
    }
    input.value = String(value);
    if (value === current.undo_history_limit) {
      return;
    }
    try {
      await this.settingsApi.update({ ...current, undo_history_limit: value });
    } catch (err) {
      console.error('Failed to update settings:', err);
      input.value = String(current.undo_history_limit);
    }
  }
}
