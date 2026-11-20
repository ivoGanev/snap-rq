import { Component, computed, inject, signal } from '@angular/core';
import { SecretsApiService } from '../core/services/secrets.service';
import { RecoveryKeyDialog } from '../dialogs/recovery-key-dialog/recovery-key-dialog';

/** Settings → Security: where the secrets key lives, and its recovery key. */
@Component({
  selector: 'app-settings-security',
  imports: [RecoveryKeyDialog],
  templateUrl: './settings-security.html',
})
export class SettingsSecurity {
  protected readonly secretsApi = inject(SecretsApiService);
  protected readonly recoveryKeyOpen = signal(false);

  protected readonly summary = computed(() => {
    switch (this.secretsApi.status()?.state) {
      case 'unlocked':
        return 'Secret variable values and stored cookies are encrypted, and unlocked on this computer.';
      case 'locked':
        return 'Secret values are locked: the key that encrypted them is missing. Unlock them from Edit variables with your recovery key.';
      case 'none':
        return 'Nothing encrypted yet. Stored cookies are encrypted automatically; tick "Secret" on a variable to encrypt its value.';
      default:
        return '';
    }
  });

  constructor() {
    void this.secretsApi.refresh();
  }
}
