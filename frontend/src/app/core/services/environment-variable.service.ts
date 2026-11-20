import { Injectable, inject, signal } from '@angular/core';
import { CommandApiService } from './command.service';
import { ChangeBus } from './change-bus.service';

export interface EnvironmentVariable {
  id: number;
  environment_id: number;
  key: string;
  /** Always empty for a secret: its value never leaves the backend unless revealed. */
  value: string;
  secret: boolean;
  /** Whether the variable has a value (the only thing known about a secret's). */
  has_value: boolean;
}

/**
 * The app's typed API for environment variables.
 */
@Injectable({ providedIn: 'root' })
export class EnvironmentVariableApiService {
  private readonly commands = inject(CommandApiService);
  private readonly bus = inject(ChangeBus);
  private environmentId: number | null = null;

  readonly variables = signal<EnvironmentVariable[]>([]);

  constructor() {
    this.bus.changesFor('variable').subscribe(() => {
      if (this.environmentId) void this.loadForEnvironment(this.environmentId);
    });
  }

  async loadForEnvironment(environmentId: number): Promise<void> {
    this.environmentId = environmentId;
    const all = await this.commands.execute<EnvironmentVariable[]>('variable.list', { environment_id: environmentId });
    this.variables.set(all ?? []);
  }

  /** Adds a plain variable; tick it as secret afterwards to encrypt it. */
  async create(variable: { environment_id: number; key: string; value: string }): Promise<EnvironmentVariable> {
    return this.commands.execute<EnvironmentVariable>('variable.create', variable);
  }

  /**
   * Changes a variable's key and, when `value` is given, its value. Leave the
   * value out to rename a secret without touching it.
   */
  async update(id: number, key: string, value?: string): Promise<EnvironmentVariable> {
    const updated = await this.commands.execute<EnvironmentVariable>('variable.update', { id, key, value });
    this.variables.update(list => list.map(v => (v.id === updated.id ? updated : v)));
    return updated;
  }

  /** Makes a variable secret (encrypting its value) or plain again. */
  async setSecret(id: number, secret: boolean): Promise<EnvironmentVariable> {
    const updated = await this.commands.execute<EnvironmentVariable>('variable.set_secret', { id, secret });
    this.variables.update(list => list.map(v => (v.id === updated.id ? updated : v)));
    return updated;
  }

  /** A secret's value, decrypted, for the reveal button. */
  async reveal(id: number): Promise<string> {
    return this.commands.execute<string>('variable.reveal', { id });
  }

  async delete(id: number): Promise<void> {
    await this.commands.execute('variable.delete', { id });
  }

}
