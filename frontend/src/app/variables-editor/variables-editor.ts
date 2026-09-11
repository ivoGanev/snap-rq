import { Component, computed, effect, ElementRef, inject, output, signal, viewChild } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { WorkspaceStateService } from '../core/services/workspace-state.service';
import { EnvironmentApiService, type Environment } from '../core/services/environment.service';
import {
  EnvironmentVariableApiService,
  type EnvironmentVariable,
} from '../core/services/environment-variable.service';

type SortColumn = 'key' | 'value';
type SortDirection = 'asc' | 'desc';

/**
 * Spreadsheet-style editor for environment variables with environment switching,
 * sorting, inline editing, and quick add/delete.
 */
@Component({
  selector: 'app-variables-editor',
  imports: [FormsModule],
  templateUrl: './variables-editor.html',
  styleUrl: './variables-editor.scss',
})
export class VariablesEditor {
  protected readonly state = inject(WorkspaceStateService);
  private readonly environmentApi = inject(EnvironmentApiService);
  private readonly variableApi = inject(EnvironmentVariableApiService);

  readonly close = output<void>();

  protected readonly environments = this.environmentApi.environments;
  protected readonly variables = this.variableApi.variables;

  readonly sortColumn = signal<SortColumn | null>(null);
  readonly sortDirection = signal<SortDirection>('asc');

  readonly newEnvironmentInputOpen = signal(false);
  readonly newEnvironmentName = signal('');
  private readonly newEnvironmentInput = viewChild<ElementRef<HTMLInputElement>>('newEnvInput');

  readonly deleteConfirmOpen = signal(false);
  readonly environmentPendingDelete = signal<Environment | null>(null);

  readonly sortedVariables = computed<EnvironmentVariable[]>(() => {
    const vars = [...this.variables()];
    const column = this.sortColumn();
    if (!column) return vars;

    const direction = this.sortDirection() === 'asc' ? 1 : -1;
    return vars.sort((a, b) => a[column].localeCompare(b[column]) * direction);
  });

  constructor() {
    effect(() => {
      const env = this.state.selectedEnvironment();
      if (env) {
        void this.variableApi.loadForEnvironment(env.id);
      } else {
        this.variableApi.variables.set([]);
      }
    });
  }

  activeEnvironmentName(): string {
    return this.state.selectedEnvironment()?.name ?? 'None';
  }

  // ---------- Sorting ----------

  toggleSort(column: SortColumn): void {
    if (this.sortColumn() === column) {
      this.sortDirection.update(d => (d === 'asc' ? 'desc' : 'asc'));
    } else {
      this.sortColumn.set(column);
      this.sortDirection.set('asc');
    }
  }

  sortIndicator(column: SortColumn): '' | 'asc' | 'desc' {
    if (this.sortColumn() !== column) return '';
    return this.sortDirection();
  }

  // ---------- Environment switching ----------

  selectEnvironment(env: Environment): void {
    this.state.selectedEnvironment.set(env);
  }

  // ---------- New environment ----------

  openNewEnvironmentInput(): void {
    this.newEnvironmentName.set('');
    this.newEnvironmentInputOpen.set(true);
    // Focus the input once it is rendered.
    setTimeout(() => {
      this.newEnvironmentInput()?.nativeElement.focus();
      this.newEnvironmentInput()?.nativeElement.select();
    }, 0);
  }

  closeNewEnvironmentInput(): void {
    this.newEnvironmentInputOpen.set(false);
    this.newEnvironmentName.set('');
  }

  async addEnvironment(): Promise<void> {
    const name = this.newEnvironmentName().trim();
    if (!name) {
      this.closeNewEnvironmentInput();
      return;
    }

    const project = this.state.selectedProject();
    if (!project) return;

    this.state.loading.set(true);
    try {
      const created = await this.environmentApi.create({
        project_id: project.id,
        name,
        created_at: new Date().toISOString(),
      });
      this.state.selectedEnvironment.set(created);
      this.closeNewEnvironmentInput();
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }

  // ---------- Environment deletion ----------

  promptDeleteEnvironment(env: Environment, event: MouseEvent): void {
    event.stopPropagation();
    this.environmentPendingDelete.set(env);
    this.deleteConfirmOpen.set(true);
  }

  cancelDeleteEnvironment(): void {
    this.environmentPendingDelete.set(null);
    this.deleteConfirmOpen.set(false);
  }

  async confirmDeleteEnvironment(): Promise<void> {
    const env = this.environmentPendingDelete();
    if (!env) return;

    this.state.loading.set(true);
    try {
      await this.environmentApi.delete(env.id);
      this.deleteConfirmOpen.set(false);
      this.environmentPendingDelete.set(null);

      // If the deleted environment was active, pick another one.
      if (this.state.selectedEnvironment()?.id === env.id) {
        const remaining = this.environments();
        this.state.selectedEnvironment.set(remaining.length > 0 ? remaining[0] : null);
      }
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }

  // ---------- Variables ----------

  async addVariable(): Promise<void> {
    const env = this.state.selectedEnvironment();
    if (!env) return;

    try {
      const created = await this.variableApi.create({
        environment_id: env.id,
        key: '',
        value: '',
      });
      this.variableApi.variables.update(list => [...list, created]);
    } catch (err) {
      console.error(err);
    }
  }

  async updateVariableField<K extends keyof EnvironmentVariable>(
    variable: EnvironmentVariable,
    field: K,
    value: EnvironmentVariable[K],
  ): Promise<void> {
    const updated = { ...variable, [field]: value };

    const list = this.variables();
    const index = list.findIndex(v => v.id === updated.id);
    if (index !== -1) {
      const next = [...list];
      next[index] = updated;
      this.variableApi.variables.set(next);
    }

    try {
      await this.variableApi.update(updated);
    } catch (err) {
      console.error(err);
    }
  }

  async deleteVariable(variable: EnvironmentVariable, event: MouseEvent): Promise<void> {
    event.stopPropagation();
    try {
      await this.variableApi.delete(variable.id);
      this.variableApi.variables.update(list => list.filter(v => v.id !== variable.id));
    } catch (err) {
      console.error(err);
    }
  }

  // ---------- Dialog ----------

  closeDialog(): void {
    this.close.emit();
  }
}
