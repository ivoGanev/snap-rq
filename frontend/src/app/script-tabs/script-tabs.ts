import { Component, computed, inject } from '@angular/core';
import { WorkspaceStateService } from '../core/services/workspace-state.service';
import { ScriptView, type ScriptTab } from '../core/workspace/workspace-view';

/**
 * The Script / Attachments tab strip at the top of a script view's middle
 * column. The tab is part of the open `ScriptView`, so switching tabs just
 * opens the same script with the other tab.
 */
@Component({
  selector: 'app-script-tabs',
  template: `
    <div class="script-tabs" role="tablist">
      @for (tab of tabs; track tab.id) {
        <button
          class="script-tab"
          type="button"
          role="tab"
          [class.is-active]="active() === tab.id"
          [attr.aria-selected]="active() === tab.id"
          (click)="open(tab.id)"
        >
          {{ tab.label }}
        </button>
      }
    </div>
  `,
  styleUrl: './script-tabs.scss',
})
export class ScriptTabs {
  private readonly state = inject(WorkspaceStateService);

  protected readonly tabs: readonly { id: ScriptTab; label: string }[] = [
    { id: 'script', label: 'Script' },
    { id: 'attachments', label: 'Attachments' },
  ];

  protected readonly active = computed(() => {
    const view = this.state.view();
    return view?.kind === 'script' ? view.tab : null;
  });

  protected open(tab: ScriptTab): void {
    const view = this.state.view();
    if (view?.kind !== 'script' || view.tab === tab) return;
    this.state.selectView(new ScriptView(view.script, tab));
  }
}
