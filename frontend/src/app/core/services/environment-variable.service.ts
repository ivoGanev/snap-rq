import { Injectable, inject, signal } from '@angular/core';
import { CommandApiService } from './command.service';
import { ChangeBus } from './change-bus.service';

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

  async create(variable: Omit<EnvironmentVariable, 'id'>): Promise<EnvironmentVariable> {
    return this.commands.execute<EnvironmentVariable>('variable.create', variable);
  }

  async update(variable: EnvironmentVariable): Promise<EnvironmentVariable> {
    const updated = await this.commands.execute<EnvironmentVariable>('variable.update', variable);
    this.variables.update(list => list.map(v => (v.id === updated.id ? updated : v)));
    return updated;
  }

  async delete(id: number): Promise<void> {
    await this.commands.execute('variable.delete', { id });
  }

}
