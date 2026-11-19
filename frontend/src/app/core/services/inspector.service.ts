import { computed, Injectable, inject } from '@angular/core';
import { WorkspaceStateService } from './workspace-state.service';
import { VIEW_DEFINITIONS } from '../models/view-definitions';
import type { InspectorContent } from '../../inspector/inspector-content';

/**
 * Decides what the Inspector shows: a multi-selection message when several
 * rows are selected, otherwise whatever the active view's rule returns (see
 * VIEW_DEFINITIONS).
 */
@Injectable({ providedIn: 'root' })
export class InspectorService {
  private readonly state = inject(WorkspaceStateService);

  readonly content = computed<InspectorContent>(() => {
    if (this.state.multiSelectionActive()) {
      return { kind: 'message', text: 'You are in multi selection mode.' };
    }

    return VIEW_DEFINITIONS[this.state.activeView().kind].inspector({
      request: this.state.selectedRequest(),
      binned: this.state.selectedBinnedRequest(),
      selectedCount: this.state.selectedIds().size,
    });
  });
}
