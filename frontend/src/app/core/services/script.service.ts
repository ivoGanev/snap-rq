import { Injectable, inject, signal } from '@angular/core';
import { CommandApiService } from './command.service';
import { ChangeBus } from './change-bus.service';
import type { HttpRequestSummary } from '../models/http';
import type { ScriptProblem } from '../models/script-problem';
import type { ScriptTryResult, TryPhase } from '../models/script-try';

export interface ScriptAppearance {
  id: number;
  script_id: number;
  appearance_type: string;
  appearance_value: string;
}

export interface Script {
  id: number;
  project_id: number;
  name: string;
  /** The script's code; only `get` returns it, lists leave it out. */
  content?: string;
  appearance: ScriptAppearance;
}

/**
 * The app's typed API for user scripts: named code attached to requests.
 * Every operation runs as a script.* command.
 */
@Injectable({ providedIn: 'root' })
export class ScriptApiService {
  private readonly commands = inject(CommandApiService);
  private readonly bus = inject(ChangeBus);
  private projectId: number | null = null;

  readonly allScripts = signal<Script[]>([]);
  readonly requestScripts = signal<Record<number, string[] | undefined>>({});

  constructor() {
    this.bus.changesFor('script').subscribe(() => {
      if (this.projectId) void this.loadScriptsForProject(this.projectId);
    });
  }

  async loadScriptsForProject(projectId: number): Promise<void> {
    this.projectId = projectId;
    const scripts = await this.commands.execute<Script[]>('script.list', { project_id: projectId });
    this.allScripts.set(scripts ?? []);
  }

  async loadScriptsForRequests(requests: HttpRequestSummary[]): Promise<void> {
    if (requests.length === 0) {
      this.requestScripts.set({});
      return;
    }
    const raw = await this.commands.execute<Record<string, string[]>>('script.list_for_requests', {
      request_ids: requests.map(r => r.id),
    });
    const mapped: Record<number, string[]> = {};
    for (const [key, value] of Object.entries(raw ?? {})) {
      mapped[Number(key)] = value ?? [];
    }
    this.requestScripts.set(mapped);
  }

  /** One script with its code, for the editor. */
  async get(id: number): Promise<Script> {
    return this.commands.execute<Script>('script.get', { id });
  }

  /** Saves a script's code. Not undoable: the editor has its own undo. */
  /**
   * The script open in the editor and its current text, saved or not: what
   * "Try it" runs.
   */
  readonly editing = signal<{ id: number; text: string } | null>(null);

  /** Runs one hook of a script's code against a request, without sending anything. */
  async try(input: {
    content: string;
    scriptName: string;
    requestId: number;
    phase: TryPhase;
  }): Promise<ScriptTryResult> {
    return this.commands.execute<ScriptTryResult>('script.try', {
      content: input.content,
      script_name: input.scriptName,
      request_id: input.requestId,
      phase: input.phase,
    });
  }

  /** Syntax errors in a script's code, from the engine that runs it (nothing runs). */
  async check(content: string): Promise<ScriptProblem[]> {
    return (await this.commands.execute<ScriptProblem[]>('script.check', { content })) ?? [];
  }

  async setContent(id: number, content: string): Promise<void> {
    await this.commands.execute('script.set_content', { id, content });
  }

  async attachScript(requestId: number, projectId: number, scriptName: string): Promise<Script> {
    const script = await this.commands.execute<Script>('script.attach', {
      request_id: requestId, project_id: projectId, script_name: scriptName,
    });
    this.allScripts.update(list => {
      if (list.some(t => t.id === script.id)) return list;
      return [...list, script];
    });
    if (requestId !== 0) {
      this.requestScripts.update(map => {
        const current = map[requestId] ?? [];
        return current.includes(script.name) ? map : { ...map, [requestId]: [...current, script.name] };
      });
    }
    return script;
  }

  async detachScript(requestId: number, scriptName: string): Promise<void> {
    await this.commands.execute('script.detach', { request_id: requestId, script_name: scriptName });
    this.requestScripts.update(map => ({
      ...map,
      [requestId]: (map[requestId] ?? []).filter(t => t !== scriptName),
    }));
  }

  async renameScript(projectId: number, oldName: string, newName: string): Promise<Script> {
    const script = await this.commands.execute<Script>('script.rename', {
      project_id: projectId, old_name: oldName, new_name: newName,
    });
    await this.loadScriptsForProject(projectId);
    return script;
  }

  async deleteScript(projectId: number, scriptName: string): Promise<void> {
    await this.commands.execute('script.delete', { project_id: projectId, script_name: scriptName });
    this.allScripts.update(list => list.filter(t => t.name !== scriptName));
    this.requestScripts.update(map => {
      const next: Record<number, string[] | undefined> = {};
      for (const [requestId, scripts] of Object.entries(map)) {
        next[Number(requestId)] = scripts?.filter(t => t !== scriptName);
      }
      return next;
    });
  }

  async getRequestsForScript(projectId: number, scriptName: string): Promise<HttpRequestSummary[]> {
    const result = await this.commands.execute<HttpRequestSummary[]>('script.list_requests', {
      project_id: projectId, script_name: scriptName,
    });
    return result ?? [];
  }

  async updateRequestOrder(projectId: number, scriptName: string, orderedRequestIds: number[]): Promise<void> {
    await this.commands.execute('script.reorder', {
      project_id: projectId, script_name: scriptName, ordered_request_ids: orderedRequestIds,
    });
  }

  async updateRequestColor(projectId: number, scriptName: string, requestIds: number[], color: string | null): Promise<void> {
    await this.commands.execute('script.set_color', {
      project_id: projectId, script_name: scriptName, request_ids: requestIds, color,
    });
  }

  /** Updates a script's appearance (icon or colour). */
  async updateAppearance(scriptId: number, appearance: Omit<ScriptAppearance, 'id' | 'script_id'>): Promise<ScriptAppearance> {
    const updated = await this.commands.execute<ScriptAppearance>('script.set_appearance', {
      script_id: scriptId, ...appearance,
    });
    this.allScripts.update(list => list.map(t => (t.id === scriptId ? { ...t, appearance: updated } : t)));
    return updated;
  }
}