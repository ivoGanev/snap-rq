import { Component, computed, inject, input, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ScriptApiService, type Script } from '../../core/services/script.service';
import { WorkspaceStateService } from '../../core/services/workspace-state.service';
import type { HttpRequestSummary } from '../../core/models/http';

/** How many matching scripts the suggestion list shows. */
const MAX_SUGGESTIONS = 6;

/**
 * Attach scripts to one request or several: type a name to pick an existing
 * script or create a new one. For a single request it also lists the
 * attached scripts, each removable.
 */
@Component({
  selector: 'app-script-picker-dialog',
  imports: [FormsModule],
  templateUrl: './script-picker-dialog.html',
  styleUrl: './script-picker-dialog.scss',
  host: {
    '(document:keydown.escape)': 'closed.emit()',
  },
})
export class ScriptPickerDialog {
  private readonly scriptApi = inject(ScriptApiService);
  private readonly state = inject(WorkspaceStateService);

  /** The requests to attach to: one from a row, several from a bulk selection. */
  readonly requests = input.required<HttpRequestSummary[]>();
  readonly closed = output<void>();

  protected readonly query = signal('');

  /** The one request, when there is exactly one; its attached scripts are shown. */
  protected readonly single = computed(() => (this.requests().length === 1 ? this.requests()[0] : null));

  protected readonly title = computed(() =>
    this.single() ? 'Scripts' : `Attach script to ${this.requests().length} requests`,
  );

  protected readonly attached = computed<string[]>(() => {
    const req = this.single();
    return req ? (this.scriptApi.requestScripts()[req.id] ?? []) : [];
  });

  protected readonly suggestions = computed<Script[]>(() => {
    const query = this.query().trim().toLowerCase();
    if (!query) return [];
    const attached = new Set(this.attached());
    return this.scriptApi
      .allScripts()
      .filter(s => !attached.has(s.name) && s.name.toLowerCase().includes(query))
      .slice(0, MAX_SUGGESTIONS);
  });

  protected async attach(scriptName: string): Promise<void> {
    const name = scriptName.trim();
    const project = this.state.selectedProject();
    if (!name || !project) return;

    this.state.loading.set(true);
    try {
      for (const req of this.requests()) {
        await this.scriptApi.attachScript(req.id, project.id, name);
      }
      this.query.set('');
    } catch (err) {
      console.error('attaching script failed', err);
    } finally {
      this.state.loading.set(false);
    }
  }

  protected async detach(scriptName: string): Promise<void> {
    const req = this.single();
    if (!req) return;
    try {
      await this.scriptApi.detachScript(req.id, scriptName);
    } catch (err) {
      console.error('detaching script failed', err);
    }
  }
}
