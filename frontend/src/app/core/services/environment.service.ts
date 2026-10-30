import { Injectable, inject, signal } from '@angular/core';
import * as EnvironmentService from '../../../../bindings/snap-rq/backend/services';
import type { Environment } from '../../../../bindings/snap-rq/backend/models';
import { CommandApiService } from './command.service';

export type { Environment };

@Injectable({ providedIn: 'root' })
export class EnvironmentApiService {
  private readonly commands = inject(CommandApiService);

  readonly environments = signal<Environment[]>([]);

  async loadForProject(projectId: number): Promise<void> {
    const all = await EnvironmentService.EnvironmentService.GetEnvironmentsForProject(projectId);
    this.environments.set(all ?? []);
  }

  async create(env: Omit<Environment, 'id'>): Promise<Environment> {
    const created = await EnvironmentService.EnvironmentService.CreateEnvironment(env as Environment);
    if (created.project_id) {
      await this.loadForProject(created.project_id);
    }
    return created;
  }

  async update(env: Environment): Promise<Environment> {
    const updated = await EnvironmentService.EnvironmentService.UpdateEnvironment(env);
    this.environments.update(list => list.map(e => (e.id === updated.id ? updated : e)));
    return updated;
  }

  async delete(id: number): Promise<void> {
    await this.commands.execute('environment.delete', { id });
  }
}
