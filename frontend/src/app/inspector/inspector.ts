import { Component, computed, inject, type Type } from '@angular/core';
import { NgComponentOutlet } from '@angular/common';
import { WorkspaceStateService } from '../core/services/workspace-state.service';
import { inspectRequest } from '../core/workspace/workspace-view';
import { MessagePane } from './panes/message-pane/message-pane';
import { ResponsePane } from './panes/response-pane/response-pane';
import { BinnedRequestPane } from './panes/binned-request-pane/binned-request-pane';
import { BinnedRequestsPane } from './panes/binned-requests-pane/binned-requests-pane';
import { ScriptPane } from './panes/script-pane/script-pane';
import type { Script } from '../core/services/script.service';

/**
 * What the Inspector can show. The open view decides which one
 * (`WorkspaceView.inspect`).
 *
 * To add a kind: add a member here, write a pane that takes it as its
 * `content` input, and add the pane to `PANES` below (it does not compile
 * until you do).
 */
export type InspectorContent =
  /** A plain message, used for empty and multi-selection states. */
  | { kind: 'message'; text: string }
  /** The responses of a live request. */
  | { kind: 'response'; requestId: number }
  /** A request in the trash bin. */
  | { kind: 'binned-request'; binId: number }
  /** Several requests in the trash bin. */
  | { kind: 'binned-requests'; binIds: number[] }
  /** A script: try its hooks against a request, and a quick reference. */
  | { kind: 'script'; script: Script };

/** The content member for one kind, e.g. `InspectorContentOf<'response'>`. */
export type InspectorContentOf<K extends InspectorContent['kind']> = Extract<InspectorContent, { kind: K }>;

const PANES: { readonly [K in InspectorContent['kind']]: Type<unknown> } = {
  message: MessagePane,
  response: ResponsePane,
  'binned-request': BinnedRequestPane,
  'binned-requests': BinnedRequestsPane,
  script: ScriptPane,
};

/** A picked group has nothing to inspect. */
const NOTHING: InspectorContent = { kind: 'message', text: '' };

const MULTI_SELECTION: InspectorContent = { kind: 'message', text: 'You are in multi selection mode.' };

/**
 * The right-hand panel: shows the pane for what the open view wants to
 * inspect. It knows nothing about any particular view.
 */
@Component({
  selector: 'app-inspector',
  imports: [NgComponentOutlet],
  template: `<ng-container *ngComponentOutlet="pane(); inputs: { content: content() }" />`,
  host: {
    class: 'sidebar sidebar-right',
    'aria-label': 'Inspector',
  },
})
export class Inspector {
  private readonly state = inject(WorkspaceStateService);

  protected readonly content = computed<InspectorContent>(() => {
    if (this.state.selectedGroupId() !== null) return NOTHING;
    const view = this.state.view();
    if (this.state.multiSelectionActive() && !(view && 'inspectsMultiSelection' in view)) return MULTI_SELECTION;

    const selection = {
      request: this.state.selectedRequest(),
      binned: this.state.selectedBinnedRequest(),
      selectedCount: this.state.selectedIds().size,
      selectedIds: [...this.state.selectedIds()],
    };
    return view ? view.inspect(selection) : inspectRequest(selection);
  });

  protected readonly pane = computed(() => PANES[this.content().kind]);
}
