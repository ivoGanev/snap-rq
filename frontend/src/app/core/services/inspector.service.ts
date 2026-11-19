import { computed, Injectable, inject } from '@angular/core';
import { WorkspaceStateService } from './workspace-state.service';
import type { InspectorContent } from '../../inspector/inspector-content';

/**
 * Decides what the Inspector shows. This is the only place that turns
 * workspace selection state into Inspector content; panes and the Inspector
 * itself never look at selection flags.
 */
@Injectable({ providedIn: 'root' })
export class InspectorService {
  private readonly state = inject(WorkspaceStateService);

  readonly content = computed<InspectorContent>(() => {
    const state = this.state;

    if (state.multiSelectionActive()) {
      return { kind: 'message', text: 'You are in multi selection mode.' };
    }

    const binned = state.selectedBinnedRequest();
    if (binned) {
      return { kind: 'binned-request', binId: binned.id };
    }

    const request = state.selectedRequest();
    if (request && !state.showingBin()) {
      return { kind: 'response', requestId: request.id };
    }

    if (!request && state.selectedIds().size === 0) {
      return { kind: 'message', text: 'Select a request to view its response.' };
    }

    // A row is selected but its full request is still loading: show nothing yet.
    return { kind: 'message', text: '' };
  });
}
