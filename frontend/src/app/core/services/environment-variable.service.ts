import { Injectable, signal } from '@angular/core';
import * as EnvironmentVariableService from '../../../../bindings/snap-rq/backend/services';
import type { EnvironmentVariable } from '../../../../bindings/snap-rq/backend/models';

export type { EnvironmentVariable };

@Injectable({ providedIn: 'root' })
export class EnvironmentVariableApiService {
  readonly variables = signal<EnvironmentVariable[]>([]);

  async loadForEnvironment(environmentId: number): Promise<void> {
    const all = await EnvironmentVariableService.EnvironmentVariableService.GetVariablesForEnvironment(environmentId);
    this.variables.set(all ?? []);
  }

  async create(variable: Omit<EnvironmentVariable, 'id'>): Promise<EnvironmentVariable> {
    return EnvironmentVariableService.EnvironmentVariableService.CreateVariable(variable as EnvironmentVariable);
  }

  async update(variable: EnvironmentVariable): Promise<EnvironmentVariable> {
    return EnvironmentVariableService.EnvironmentVariableService.UpdateVariable(variable);
  }

  async delete(id: number): Promise<void> {
    return EnvironmentVariableService.EnvironmentVariableService.DeleteVariable(id);
  }
}
