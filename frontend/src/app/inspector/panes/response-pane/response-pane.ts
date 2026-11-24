import { Component, DestroyRef, effect, inject, input } from '@angular/core';
import { ResponseViewer } from '../../../response-viewer/response-viewer';
import { WorkspaceStateService } from '../../../core/services/workspace-state.service';
import { RequestApiService } from '../../../core/services/request.service';
import type { InspectorContentOf } from '../../inspector';

/** Inspector pane for a live request: loads and shows its responses. */
@Component({
  selector: 'app-response-pane',
  imports: [ResponseViewer],
  template: `
    <app-response-viewer [canClearHistory]="true" />
  `,
  styles: `
    :host {
      display: contents;
    }
  `,
})
export class ResponsePane {
  private readonly state = inject(WorkspaceStateService);
  private readonly requestApi = inject(RequestApiService);

  readonly content = input.required<InspectorContentOf<'response'>>();

  constructor() {
    // Reads the whole content object on purpose: the Inspector hands over a new
    // one whenever the selected request is refreshed, which reloads responses
    // just like before.
    effect(() => {
      void this.state.loadResponses(this.content().requestId);
    });

    // Leave no stale responses behind for the next request or pane.
    inject(DestroyRef).onDestroy(() => {
      this.requestApi.responses.set([]);
      this.state.selectedResponse.set(null);
      this.state.responsesLoading.set(false);
    });
  }
}
