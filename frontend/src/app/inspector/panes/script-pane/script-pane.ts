import { Component, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { Browser } from '@wailsio/runtime';
import { InspectorHeader } from '../../inspector-header/inspector-header';
import { ScriptLog } from '../../../script-log/script-log';
import { Select, type SelectOption } from '../../../select/select';
import type { InspectorContentOf } from '../../inspector';
import { ScriptApiService } from '../../../core/services/script.service';
import { RequestApiService, type HttpRequestSummary } from '../../../core/services/request.service';
import { CommandApiService } from '../../../core/services/command.service';
import { WorkspaceStateService } from '../../../core/services/workspace-state.service';
import type { HttpResponse } from '../../../core/models/http';
import type { TryOutcome, TryPhase } from '../../../core/models/script-try';
import { SCRIPT_EXAMPLES } from './script-examples';

/** Placeholder until there is a real donation page (plan D4). */
const DONATE_URL = 'https://example.com/donate';

/**
 * Inspector pane of a script view: try the script against a request without
 * sending anything (beforeRequest shows the request it would produce,
 * afterResponse runs against the request's last response), or send it for
 * real. A reference (the API, and examples in tabs) folds out below.
 */
@Component({
  selector: 'app-script-pane',
  imports: [InspectorHeader, ScriptLog, Select],
  templateUrl: './script-pane.html',
  styleUrl: './script-pane.scss',
})
export class ScriptPane {
  private readonly scriptApi = inject(ScriptApiService);
  private readonly requestApi = inject(RequestApiService);
  private readonly commands = inject(CommandApiService);
  private readonly state = inject(WorkspaceStateService);

  readonly content = input.required<InspectorContentOf<'script'>>();

  protected readonly script = computed(() => this.content().script);
  /** The requests this script is attached to, offered first. */
  protected readonly attached = signal<HttpRequestSummary[]>([]);
  /** Every other request of the project: trying doesn't need attaching. */
  protected readonly others = computed(() => {
    const attachedIds = new Set(this.attached().map((r) => r.id));
    return this.requestApi.forProject(this.script().project_id).filter((r) => !attachedIds.has(r.id));
  });
  protected readonly requestId = signal<number | null>(null);
  protected readonly requestOptions = computed<SelectOption<number>[]>(() => [
    ...this.attached().map((r) => ({ value: r.id, label: `${r.method} ${r.name}`, group: 'Attached to this script' })),
    ...this.others().map((r) => ({ value: r.id, label: `${r.method} ${r.name}`, group: 'Other requests' })),
  ]);
  protected readonly busy = signal(false);
  protected readonly outcome = signal<TryOutcome | null>(null);
  protected readonly failure = signal('');

  protected readonly donateUrl = DONATE_URL;

  /** The reference: the API list, or one of the examples. */
  protected readonly examples = SCRIPT_EXAMPLES;
  protected readonly referenceTab = signal('api');
  protected readonly example = computed(() => this.examples.find((e) => e.id === this.referenceTab()) ?? null);
  protected readonly copied = signal(false);
  private copiedTimer: ReturnType<typeof setTimeout> | null = null;

  constructor() {
    // A different script: its attachments, a fresh result.
    effect(() => {
      const script = this.script();
      untracked(() => {
        this.outcome.set(null);
        this.failure.set('');
        void this.loadAttached(script.project_id, script.name);
      });
    });
  }

  protected selectRequest(id: number): void {
    this.requestId.set(id);
    this.outcome.set(null);
  }

  /** Runs one hook of the editor's current code against the chosen request. */
  protected async run(phase: TryPhase): Promise<void> {
    const requestId = this.requestId();
    if (requestId === null) return;
    await this.busyWhile(async () => {
      const script = this.script();
      const result = await this.scriptApi.try({
        content: await this.currentCode(),
        scriptName: script.name,
        requestId,
        phase,
      });
      this.outcome.set({ kind: 'try', phase, result });
    });
  }

  /** Sends the chosen request for real (all its scripts run) and shows that response. */
  protected async sendForReal(event: Event): Promise<void> {
    const requestId = this.requestId();
    if (requestId === null) return;
    await this.busyWhile(async () => {
      // A real send runs the saved code: save what's typed first.
      const script = this.script();
      const editing = this.scriptApi.editing();
      if (editing?.id === script.id) await this.scriptApi.setContent(script.id, editing.text);
      await this.state.sendRequest(await this.requestApi.getFull(requestId), event);
      const [latest] = await this.commands.execute<HttpResponse[]>('response.list_for_request', {
        request_id: requestId,
      });
      if (latest) this.outcome.set({ kind: 'sent', response: latest });
    });
  }

  /** Copies an example, to paste into the editor. */
  protected async copy(code: string): Promise<void> {
    try {
      await navigator.clipboard.writeText(code);
      this.copied.set(true);
      if (this.copiedTimer) clearTimeout(this.copiedTimer);
      this.copiedTimer = setTimeout(() => this.copied.set(false), 1500);
    } catch (err) {
      console.error('copying the example failed', err);
    }
  }

  protected openDonate(event: MouseEvent): void {
    event.preventDefault();
    void Browser.OpenURL(DONATE_URL);
  }

  /** The editor's text when it has this script open (saved or not), else the saved code. */
  private async currentCode(): Promise<string> {
    const editing = this.scriptApi.editing();
    if (editing?.id === this.script().id) return editing.text;
    return (await this.scriptApi.get(this.script().id)).content ?? '';
  }

  private async loadAttached(projectId: number, name: string): Promise<void> {
    try {
      const attached = await this.scriptApi.getRequestsForScript(projectId, name);
      if (this.script().name !== name) return;
      this.attached.set(attached);
      const current = this.requestId();
      const all = [...attached, ...this.others()];
      if (current === null || !all.some((r) => r.id === current)) {
        this.requestId.set(all[0]?.id ?? null);
      }
    } catch (err) {
      console.error('loading the script attachments failed', err);
    }
  }

  private async busyWhile(work: () => Promise<void>): Promise<void> {
    this.busy.set(true);
    this.failure.set('');
    try {
      await work();
    } catch (err) {
      this.failure.set(err instanceof Error ? err.message : String(err));
    } finally {
      this.busy.set(false);
    }
  }
}
