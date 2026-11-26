import { Injectable, inject, signal } from '@angular/core';
import { CommandApiService } from './command.service';
import { ChangeBus } from './change-bus.service';

export interface Project {
  id: number;
  profile_id: number;
  name: string;
  /** The environment the project uses; 0 is none. */
  selected_environment_id: number;
}

/**
 * The app's typed API for projects.
 */
@Injectable({ providedIn: 'root' })
export class ProjectApiService {
  private readonly commands = inject(CommandApiService);
  private readonly bus = inject(ChangeBus);

  readonly projects = signal<Project[]>([]);

  constructor() {
    this.bus.changesFor('project').subscribe(() => void this.loadAll());
  }

  async loadAll(): Promise<void> {
    const all = await this.commands.execute<Project[]>('project.list', {});
    this.projects.set(all ?? []);
  }

  async create(project: Pick<Project, 'profile_id' | 'name'>): Promise<Project> {
    const created = await this.commands.execute<Project>('project.create', project);
    await this.loadAll();
    return created;
  }

  async delete(id: number): Promise<number[]> {
    const result = await this.commands.execute<{ deleted: number }>('project.delete', { id });
    return result?.deleted ? [id] : [id];
  }
}