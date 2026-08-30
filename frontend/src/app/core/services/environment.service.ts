import { Injectable, signal } from '@angular/core';
import * as EnvironmentService from '../../../../bindings/snap-rq/backend/services';
import type { Environment } from '../../../../bindings/snap-rq/backend/models';

export type { Environment };

@Injectable({ providedIn: 'root' })
export class EnvironmentApiService {
  readonly environments = signal<Environment[]>([]);

  async loadForProject(projectId: number): Promise<void> {
    const all = await EnvironmentService.EnvironmentService.GetEnvironmentsForProject(projectId);
    this.environments.set(all ?? []);
  }
}
