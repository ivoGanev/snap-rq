import { Component, inject, input, output, signal, type OnInit } from '@angular/core';
import { Clipboard, Dialogs } from '@wailsio/runtime';
import { SecretsApiService } from '../../core/services/secrets.service';

/** Why the recovery key is shown: the first secret, a replaced key, or asked for in Settings. */
export type RecoveryKeyReason = 'first' | 'changed' | 'manual';

/**
 * Shows the recovery key with Copy and Save: once after the first secret, once
 * at startup after the key was replaced, or from Settings → Security. Showing
 * it marks it seen, so it isn't announced again.
 */
@Component({
  selector: 'app-recovery-key-dialog',
  templateUrl: './recovery-key-dialog.html',
  styleUrl: './recovery-key-dialog.scss',
  host: {
    '(document:keydown.escape)': 'closed.emit()',
  },
})
export class RecoveryKeyDialog implements OnInit {
  private readonly secretsApi = inject(SecretsApiService);

  readonly reason = input<RecoveryKeyReason>('manual');
  readonly closed = output<void>();

  protected readonly recoveryKey = signal('');
  protected readonly notice = signal('');

  async ngOnInit(): Promise<void> {
    try {
      this.recoveryKey.set(await this.secretsApi.recoveryKey());
      await this.secretsApi.markRecoveryKeySeen();
    } catch (err) {
      console.error('loading the recovery key failed', err);
      this.notice.set("The recovery key can't be shown right now.");
    }
  }

  protected async copy(): Promise<void> {
    await Clipboard.SetText(this.recoveryKey());
    this.notice.set('Copied. Paste it into your password manager.');
  }

  protected async save(): Promise<void> {
    try {
      const path = await Dialogs.SaveFile({
        Filename: 'snaprq-recovery-key.txt',
        Title: 'Save recovery key',
        ButtonText: 'Save',
      });
      if (!path) return;
      await this.secretsApi.saveRecoveryKey(path);
      this.notice.set('Saved. Keep the file somewhere safe, away from this computer.');
    } catch (err) {
      console.error('saving the recovery key failed', err);
      this.notice.set("Couldn't save the file.");
    }
  }
}
