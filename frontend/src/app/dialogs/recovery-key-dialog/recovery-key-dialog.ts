import { Component, inject, input, output, signal, type OnInit } from '@angular/core';
import { Clipboard, Dialogs } from '@wailsio/runtime';
import { SecretsApiService } from '../../core/services/secrets.service';

/**
 * Shows the recovery key with Copy and Save, either right after the first
 * secret is created or from Settings → Security.
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

  /** True right after the first secret: the text explains why the key matters. */
  readonly firstTime = input(false);
  readonly closed = output<void>();

  protected readonly recoveryKey = signal('');
  protected readonly notice = signal('');

  async ngOnInit(): Promise<void> {
    try {
      this.recoveryKey.set(await this.secretsApi.recoveryKey());
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
