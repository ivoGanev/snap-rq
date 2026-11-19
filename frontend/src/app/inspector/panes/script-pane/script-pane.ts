import { Component, DestroyRef, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { Browser } from '@wailsio/runtime';
import { InspectorHeader } from '../../inspector-header/inspector-header';
import { CodeEditor } from '../../../code-editor/code-editor';
import { ScriptApiService } from '../../../core/services/script.service';
import type { InspectorContentOf } from '../../inspector';

/** Placeholder until there is a real donation page (plan D4). */
const DONATE_URL = 'https://example.com/donate';
/** How long typing must pause before the script is saved. */
const SAVE_DELAY_MS = 500;

/**
 * Inspector pane for a user script: its code in a JavaScript editor, saved
 * automatically. Scripts cannot run yet, which the banner says.
 */
@Component({
  selector: 'app-script-pane',
  imports: [InspectorHeader, CodeEditor],
  templateUrl: './script-pane.html',
  styleUrl: './script-pane.scss',
})
export class ScriptPane {
  private readonly scriptApi = inject(ScriptApiService);

  readonly content = input.required<InspectorContentOf<'script'>>();
  /**
   * The Inspector hands over a new content object on every selection change;
   * this only changes when the script does, so selecting a request in the
   * script view does not reload the editor.
   */
  private readonly scriptId = computed(() => this.content().scriptId);

  protected readonly name = signal('');
  /** The text the editor starts from; typing does not write back to it. */
  protected readonly loadedText = signal('');
  protected readonly loaded = signal(false);
  protected readonly saveError = signal(false);
  protected readonly donateUrl = DONATE_URL;

  private pending: { id: number; text: string } | null = null;
  private timer: ReturnType<typeof setTimeout> | null = null;

  constructor() {
    effect(() => {
      const id = this.scriptId();
      untracked(() => void this.load(id));
    });

    // Keep the heading in step with renames from the sidebar.
    effect(() => {
      const id = this.scriptId();
      const script = this.scriptApi.allScripts().find(s => s.id === id);
      if (script) this.name.set(script.name);
    });

    inject(DestroyRef).onDestroy(() => void this.flush());
  }

  protected onTextChange(text: string): void {
    this.pending = { id: this.scriptId(), text };
    if (this.timer) clearTimeout(this.timer);
    this.timer = setTimeout(() => void this.flush(), SAVE_DELAY_MS);
  }

  protected openDonate(event: MouseEvent): void {
    event.preventDefault();
    void Browser.OpenURL(DONATE_URL);
  }

  private async load(id: number): Promise<void> {
    // Save what was typed into the previous script before switching.
    await this.flush();
    this.loaded.set(false);
    try {
      const script = await this.scriptApi.get(id);
      if (this.scriptId() !== id) return;
      this.name.set(script.name);
      this.loadedText.set(script.content ?? '');
      this.loaded.set(true);
    } catch (err) {
      console.error('loading script failed', err);
    }
  }

  private async flush(): Promise<void> {
    if (this.timer) {
      clearTimeout(this.timer);
      this.timer = null;
    }
    const pending = this.pending;
    if (!pending) return;
    this.pending = null;
    try {
      await this.scriptApi.setContent(pending.id, pending.text);
      this.saveError.set(false);
    } catch (err) {
      console.error('saving script failed', err);
      this.saveError.set(true);
    }
  }
}
