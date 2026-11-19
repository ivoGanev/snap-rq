import { Component, effect, inject } from '@angular/core';
import { WorkspaceStateService } from '../core/services/workspace-state.service';
import { RequestApiService } from '../core/services/request.service';
import { ResponseViewer } from '../response-viewer/response-viewer';
import { BinnedRequestDetail } from '../binned-request-detail/binned-request-detail';

/**
 * The right-hand panel. It is a container: it decides which detail component
 * to show for what the user is doing (a request's responses, a request in the
 * trash bin) and owns the panel header and empty states.
 */
@Component({
  selector: 'app-inspector',
  imports: [ResponseViewer, BinnedRequestDetail],
  templateUrl: './inspector.html',
  styleUrl: './inspector.scss',
  host: {
    class: 'sidebar sidebar-right',
    'aria-label': 'Inspector',
  },
})
export class Inspector {
  protected readonly state = inject(WorkspaceStateService);
  private readonly requestApi = inject(RequestApiService);

  protected readonly responses = this.requestApi.responses;

  constructor() {
    // Keep the selected request's responses loaded; clear them whenever no
    // single live request is selected.
    effect(() => {
      if (this.state.multiSelectionActive() || this.state.showingBin()) {
        this.requestApi.responses.set([]);
        this.state.selectedResponse.set(null);
        this.state.responsesLoading.set(false);
        return;
      }

      const req = this.state.selectedRequest();
      if (req) {
        this.state.loadResponses(req.id);
      } else {
        this.requestApi.responses.set([]);
        this.state.selectedResponse.set(null);
        this.state.responsesLoading.set(this.state.selectedIds().size > 0);
      }
    });
  }
}
