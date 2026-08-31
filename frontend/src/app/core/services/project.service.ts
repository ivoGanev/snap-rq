import { Injectable, signal } from '@angular/core';
import * as ProjectService from '../../../../bindings/snap-rq/backend/services';
import type { Project } from '../../../../bindings/snap-rq/backend/models';

export type { Project };

@Injectable({ providedIn: 'root' })
export class ProjectApiService {
  readonly projects = signal<Project[]>([]);

  async loadAll(): Promise<void> {
    const all = await ProjectService.ProjectService.GetAllProjects();
    this.projects.set(all ?? []);
  }

  async create(project: Omit<Project, 'id'>): Promise<Project> {
    const created = await ProjectService.ProjectService.CreateProject(project as Project);
    await this.loadAll();
    return created;
  }

  async delete(id: number): Promise<number[]> {
    const deletedRequestIds = await ProjectService.ProjectService.DeleteProject(id);
    await this.loadAll();
    return (deletedRequestIds ?? []).map(id => Number(id));
  }
}
