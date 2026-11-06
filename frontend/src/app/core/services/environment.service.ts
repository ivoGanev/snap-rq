import { Injectable, inject, signal } from '@angular/core';
import { CommandApiService } from './command.service';
import { ChangeBus } from './change-bus.service';

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
  private readonly bus = inject(ChangeBus);
  private projectId: number | null = null;
  
  readonly environments = signal<Environment[]>([]);

  constructor() {
    this.bus.changesFor('environment').subscribe(() => {
      if (this.projectId) void this.loadForProject(this.projectId);
    });
  }

  async loadForProject(projectId: number): Promise<void> {
    this.projectId = projectId;
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

}
