import { Injectable, inject, signal } from '@angular/core';
import { CommandApiService } from './command.service';

export interface Environment {
  id: number;
  project_id: number;
  name: string;
  created_at: string;
}

/**
 * The app's typed API for environments.
 */
@Injectable({ providedIn: 'root' })
export class EnvironmentApiService {
  private readonly commands = inject(CommandApiService);
  
  readonly environments = signal<Environment[]>([]);

  async loadForProject(projectId: number): Promise<void> {
    const all = await this.commands.execute<Environment[]>('environment.list', { project_id: projectId });
    this.environments.set(all ?? []);
  }

  async create(env: Omit<Environment, 'id'>): Promise<Environment> {
    const created = await this.commands.execute<Environment>('environment.create', env);
    if (created.project_id) {
      await this.loadForProject(created.project_id);
    }
    return created;
  }

  async update(env: Environment): Promise<Environment> {
    return this.commands.execute<Environment>('environment.update', env);
  }

  async delete(id: number): Promise<void> {
    await this.commands.execute('environment.delete', { id });
  }

  /** Refreshes the environment store from a command change. */
}
