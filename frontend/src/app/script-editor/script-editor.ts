import { Component, computed, effect, inject, signal, untracked } from '@angular/core';
import { CodeEditor } from '../code-editor/code-editor';
import { ScriptTabs } from '../script-tabs/script-tabs';
import { ScriptApiService } from '../core/services/script.service';
import { WorkspaceStateService } from '../core/services/workspace-state.service';

/**
 * The middle column of a script view: the open script's code in a
 * JavaScript editor, saved automatically (not part of app undo; the editor
 * has its own).
 */
@Component({
  selector: 'app-script-editor',
  imports: [CodeEditor, ScriptTabs],
  templateUrl: './script-editor.html',
  styleUrl: './script-editor.scss',
  host: {
    class: 'main-column',
    'aria-label': 'Script editor',
  },
})
export class ScriptEditor {
  private readonly state = inject(WorkspaceStateService);
  private readonly scriptApi = inject(ScriptApiService);

  /** The open script's id; a plain number, so unrelated view updates don't reload. */
  private readonly scriptId = computed(() => {
    const view = this.state.view();
    return view?.kind === 'script' ? view.script.id : null;
  });

  /** Follows renames from the sidebar. */
  protected readonly name = computed(() => {
    const id = this.scriptId();
    return this.scriptApi.allScripts().find(s => s.id === id)?.name ?? this.state.view()?.title ?? '';
  });
  /** For ctx.env.get('…') completions: the selected environment's variables. */
  protected readonly environmentVariables = this.state.environmentVariables;
  /** Syntax errors as you type, from the engine that runs scripts. */
  protected readonly check = (code: string) => this.scriptApi.check(code);

  /** The text the editor starts from; typing does not write back to it. */
  protected readonly loadedText = signal('');
  protected readonly loaded = signal(false);
  protected readonly saveError = signal(false);


  constructor() {
    effect(() => {
      const id = this.scriptId();
      untracked(() => void this.load(id));
    });
  }

  protected onTextChange(text: string): void {
    const id = this.scriptId();
    if (id === null) return;
    this.scriptApi.editing.set({ id, text });
    void this.save(id, text);
  }

  private async load(id: number | null): Promise<void> {
    this.loaded.set(false);
    if (id === null) return;
    try {
      const script = await this.scriptApi.get(id);
      if (this.scriptId() !== id) return;
      this.loadedText.set(script.content ?? '');
      this.scriptApi.editing.set({ id, text: script.content ?? '' });
      this.loaded.set(true);
    } catch (err) {
      console.error('loading script failed', err);
    }
  }

  private async save(id: number, text: string): Promise<void> {
    try {
      await this.scriptApi.setContent(id, text);
      this.saveError.set(false);
    } catch (err) {
      console.error('saving script failed', err);
      this.saveError.set(true);
    }
  }
}
