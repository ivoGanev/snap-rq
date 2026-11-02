import { Injectable, inject, signal } from '@angular/core';
import { CommandApiService } from './command.service';

export interface EnvironmentVariable {
  id: number;
  environment_id: number;
  key: string;
  value: string;
}

/**
 * The app's typed API for environment variables.
 */
@Injectable({ providedIn: 'root' })
export class EnvironmentVariableApiService {
  private readonly commands = inject(CommandApiService);
  
  readonly variables = signal<EnvironmentVariable[]>([]);

  async loadForEnvironment(environmentId: number): Promise<void> {
    const all = await this.commands.execute<EnvironmentVariable[]>('variable.list', { environment_id: environmentId });
    this.variables.set(all ?? []);
  }

  async create(variable: Omit<EnvironmentVariable, 'id'>): Promise<EnvironmentVariable> {
    return this.commands.execute<EnvironmentVariable>('variable.create', variable);
  }

  async update(variable: EnvironmentVariable): Promise<EnvironmentVariable> {
    return this.commands.execute<EnvironmentVariable>('variable.update', variable);
  }

  async delete(id: number): Promise<void> {
    await this.commands.execute('variable.delete', { id });
  }

  /** Refreshes the variable store from a command change. */
}
