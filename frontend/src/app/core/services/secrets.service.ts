import { Injectable, inject, signal } from '@angular/core';
import { CommandApiService } from './command.service';

/** none: no secret yet · unlocked: usable · locked: the key is missing or different. */
export type SecretsState = 'none' | 'unlocked' | 'locked';

export interface SecretsStatus {
  state: SecretsState;
  /** Where the key lives: the OS credential store, or a file next to the database. */
  store: 'keyring' | 'file' | 'memory';
  /** How many variables are marked secret: the locked banner needs some to unlock. */
  secret_variables: number;
  recovery_key: RecoveryKeyState;
}

/** Whether the current recovery key was shown: never, yes, or an older key was. */
export type RecoveryKeyState = 'unseen' | 'seen' | 'changed';

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

  /**
   * The startup check: 'changed' means the key was replaced since the user
   * saw a recovery key. Doesn't read the key unless one was seen before.
   */
  async recoveryKeyState(): Promise<RecoveryKeyState> {
    return this.commands.execute<RecoveryKeyState>('secrets.recovery_key_state', {});
  }

  /** Remembers that the current recovery key was shown, so it isn't announced again. */
  async markRecoveryKeySeen(): Promise<void> {
    await this.commands.execute('secrets.mark_recovery_key_seen', {});
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

  /** Clears every secret value (names and flags stay) and every cookie, so secrets work again. */
  async reset(): Promise<void> {
    await this.commands.execute('secrets.reset', {});
    await this.refresh();
  }
}
