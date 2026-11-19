import { Component, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { Browser } from '@wailsio/runtime';
import { InspectorHeader } from '../../inspector-header/inspector-header';
import { ScriptApiService } from '../../../core/services/script.service';
import { WorkspaceStateService } from '../../../core/services/workspace-state.service';
import { ChangeBus } from '../../../core/services/change-bus.service';
import type { HttpRequestSummary } from '../../../core/models/http';
import type { InspectorContentOf } from '../../inspector';

/** Placeholder until there is a real donation page (plan D4). */
const DONATE_URL = 'https://example.com/donate';

/**
 * Inspector pane of a script view: says scripts can't run yet (with a
 * donate link) and lists the requests the script is attached to.
 */
@Component({
  selector: 'app-script-info-pane',
  imports: [InspectorHeader],
  templateUrl: './script-info-pane.html',
  styleUrl: './script-info-pane.scss',
})
export class ScriptInfoPane {
  private readonly scriptApi = inject(ScriptApiService);
  private readonly state = inject(WorkspaceStateService);
  private readonly changeBus = inject(ChangeBus);

  readonly content = input.required<InspectorContentOf<'script-info'>>();

  protected readonly donateUrl = DONATE_URL;
  protected readonly attached = signal<HttpRequestSummary[]>([]);

  /**
   * The script's current name (it can change by undo while the view holds the
   * old one). A plain string, so it only changes when the script or its name does.
   */
  private readonly scriptName = computed(() => {
    const { scriptId, scriptName } = this.content();
    return this.scriptApi.allScripts().find(s => s.id === scriptId)?.name ?? scriptName;
  });

  constructor() {
    // Reload for another script and after any command (attach, detach,
    // rename, undo...).
    effect(() => {
      const name = this.scriptName();
      const projectId = this.state.selectedProject()?.id ?? null;
      this.changeBus.revision();
      untracked(() => void this.load(projectId, name));
    });
  }

  protected openDonate(event: MouseEvent): void {
    event.preventDefault();
    void Browser.OpenURL(DONATE_URL);
  }

  private async load(projectId: number | null, name: string): Promise<void> {
    if (projectId === null) return;
    try {
      const requests = await this.scriptApi.getRequestsForScript(projectId, name);
      if (this.scriptName() === name) this.attached.set(requests);
    } catch (err) {
      console.error('loading attached requests failed', err);
    }
  }
}
