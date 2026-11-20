import { Injectable, inject, signal } from '@angular/core';
import { CommandApiService } from './command.service';

/** none: no secret yet · unlocked: usable · locked: the key is missing or different. */
export type SecretsState = 'none' | 'unlocked' | 'locked';

export interface SecretsStatus {
  state: SecretsState;
  /** Where the key lives: the OS credential store, or a file next to the database. */
  store: 'keyring' | 'file' | 'memory';
}

/**
 * The key that encrypts secret variable values: its status and the recovery
 * key. See docs/plans/secret-variables.md.
 */
@Injectable({ providedIn: 'root' })
export class SecretsApiService {
  private readonly commands = inject(CommandApiService);

  readonly status = signal<SecretsStatus | null>(null);

  async refresh(): Promise<SecretsStatus> {
    const status = await this.commands.execute<SecretsStatus>('secrets.status', {});
    this.status.set(status);
    return status;
  }

  async recoveryKey(): Promise<string> {
    return this.commands.execute<string>('secrets.recovery_key', {});
  }

  async saveRecoveryKey(filePath: string): Promise<void> {
    await this.commands.execute('secrets.save_recovery_key', { file_path: filePath });
  }

  /** Puts a recovery key back; throws when it doesn't match these secrets. */
  async restore(recoveryKey: string): Promise<void> {
    await this.commands.execute('secrets.restore', { recovery_key: recoveryKey });
    await this.refresh();
  }

  /** Clears every secret value (names and flags stay) so secrets work again. */
  async reset(): Promise<void> {
    await this.commands.execute('secrets.reset', {});
    await this.refresh();
  }
}
