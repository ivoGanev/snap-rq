import {
  Component,
  computed,
  effect,
  ElementRef,
  inject,
  output,
  signal,
  viewChild,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { WorkspaceStateService } from '../core/services/workspace-state.service';
import { EnvironmentApiService, type Environment } from '../core/services/environment.service';
import {
  EnvironmentVariableApiService,
  type EnvironmentVariable,
} from '../core/services/environment-variable.service';
import { ConfirmService } from '../core/services/confirm.service';
import { SecretsApiService } from '../core/services/secrets.service';
import { RecoveryKeyDialog } from '../dialogs/recovery-key-dialog/recovery-key-dialog';

type SortColumn = 'key' | 'value';
type SortDirection = 'asc' | 'desc';

/**
 * Spreadsheet-style editor for environment variables with environment switching,
 * sorting, inline editing, and quick add/delete.
 */
@Component({
  selector: 'app-variables-editor',
  imports: [FormsModule, RecoveryKeyDialog],
  templateUrl: './variables-editor.html',
  styleUrl: './variables-editor.scss',
})
export class VariablesEditor {
  protected readonly state = inject(WorkspaceStateService);
  private readonly environmentApi = inject(EnvironmentApiService);
  private readonly variableApi = inject(EnvironmentVariableApiService);
  private readonly confirm = inject(ConfirmService);
  protected readonly secretsApi = inject(SecretsApiService);

  readonly close = output<void>();

  protected readonly environments = this.environmentApi.environments;
  protected readonly variables = this.variableApi.variables;

  readonly sortColumn = signal<SortColumn | null>(null);
  readonly sortDirection = signal<SortDirection>('asc');

  readonly newEnvironmentInputOpen = signal(false);
  readonly newEnvironmentName = signal('');
  private readonly newEnvironmentInput = viewChild<ElementRef<HTMLInputElement>>('newEnvInput');

  /**
   * Revealed secret values by variable id. Only lives while the editor is
   * open on this environment; the eye button adds and removes entries.
   */
  protected readonly revealed = signal<ReadonlyMap<number, string>>(new Map());
  /** The masked secret field being typed into, which hints that typing replaces it. */
  protected readonly focusedSecret = signal<number | null>(null);
  /** Shown once, right after the very first secret is created. */
  protected readonly recoveryKeyOpen = signal(false);
  protected readonly recoveryInput = signal('');
  protected readonly unlockError = signal('');

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
    void this.secretsApi.refresh();

    // Switching environment hides every revealed secret again.
    effect(() => {
      this.state.selectedEnvironment();
      this.revealed.set(new Map());
    });
  }

  activeEnvironmentName(): string {
    return this.state.selectedEnvironment()?.name ?? 'None';
  }

  isDefaultEnvironment(env: Environment): boolean {
    return env.name.toLowerCase() === 'default';
  }

  // ---------- Sorting ----------

  toggleSort(column: SortColumn): void {
    if (this.sortColumn() === column) {
      this.sortDirection.update((d) => (d === 'asc' ? 'desc' : 'asc'));
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

    if (env && this.isDefaultEnvironment(env)) {
      this.environmentPendingDelete.set(null);
      this.deleteConfirmOpen.set(false);
      return;
    }

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
      this.variableApi.variables.update((list) => [...list, created]);
    } catch (err) {
      console.error(err);
    }
  }

  async updateKey(variable: EnvironmentVariable, key: string): Promise<void> {
    this.patchLocal(variable.id, { key });
    try {
      // A secret's value isn't here; leaving it out keeps it as it is.
      await this.variableApi.update(variable.id, key, variable.secret ? undefined : variable.value);
    } catch (err) {
      console.error(err);
    }
  }

  async updateValue(variable: EnvironmentVariable, value: string): Promise<void> {
    if (variable.secret) {
      // The new value replaces the secret; keep it visible only if it was revealed.
      if (this.revealed().has(variable.id)) this.setRevealed(variable.id, value);
      this.patchLocal(variable.id, { has_value: value !== '' });
    } else {
      this.patchLocal(variable.id, { value, has_value: value !== '' });
    }
    try {
      await this.variableApi.update(variable.id, variable.key, value);
    } catch (err) {
      console.error(err);
    }
  }

  // ---------- Secrets ----------

  protected secretPlaceholder(variable: EnvironmentVariable): string {
    if (this.focusedSecret() === variable.id) return 'Enter new value…';
    return variable.has_value ? '••••••••••••' : 'Value';
  }

  async toggleSecret(variable: EnvironmentVariable): Promise<void> {
    try {
      // Cookies usually create the key first, so "first" means the first secret variable.
      const wasFirst = !variable.secret && (await this.secretsApi.refresh()).secret_variables === 0;
      await this.variableApi.setSecret(variable.id, !variable.secret);
      this.setRevealed(variable.id, null);
      const status = await this.secretsApi.refresh();
      // The first secret just created the key: show the recovery key once.
      if (wasFirst && status.state === 'unlocked') this.recoveryKeyOpen.set(true);
    } catch (err) {
      console.error(err);
    }
  }

  async unlock(): Promise<void> {
    const key = this.recoveryInput().trim();
    if (!key) return;
    try {
      await this.secretsApi.restore(key);
      this.recoveryInput.set('');
      this.unlockError.set('');
    } catch {
      this.unlockError.set("That recovery key doesn't unlock these secrets.");
    }
  }

  async startOver(): Promise<void> {
    const confirmed = await this.confirm.ask({
      title: 'Clear all secret values?',
      message:
        'Every secret value in every environment is cleared, and so are stored cookies. The variable names stay, and you type the values again.',
      confirmLabel: 'Clear secret values',
    });
    if (!confirmed) return;
    try {
      await this.secretsApi.reset();
      this.revealed.set(new Map());
    } catch (err) {
      console.error(err);
    }
  }

  async toggleReveal(variable: EnvironmentVariable): Promise<void> {
    if (this.revealed().has(variable.id)) {
      this.setRevealed(variable.id, null);
      return;
    }
    try {
      this.setRevealed(variable.id, await this.variableApi.reveal(variable.id));
    } catch (err) {
      console.error(err);
    }
  }

  private setRevealed(id: number, value: string | null): void {
    this.revealed.update((map) => {
      const next = new Map(map);
      if (value === null) next.delete(id);
      else next.set(id, value);
      return next;
    });
  }

  private patchLocal(id: number, changes: Partial<EnvironmentVariable>): void {
    this.variableApi.variables.update((list) => list.map((v) => (v.id === id ? { ...v, ...changes } : v)));
  }

  async deleteVariable(variable: EnvironmentVariable, event: MouseEvent): Promise<void> {
    event.stopPropagation();
    try {
      await this.variableApi.delete(variable.id);
      this.variableApi.variables.update((list) => list.filter((v) => v.id !== variable.id));
    } catch (err) {
      console.error(err);
    }
  }

  // ---------- Dialog ----------

  closeDialog(): void {
    this.close.emit();
  }
}
