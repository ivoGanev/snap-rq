import { Component, effect, inject } from '@angular/core';
import { WorkspaceStateService, type RightPanelMode } from '../core/services/workspace-state.service';
import { RequestApiService } from '../core/services/request.service';
import { RequestEditor } from '../request-editor/request-editor';
import { ResponseViewer } from '../response-viewer/response-viewer';

@Component({
  selector: 'app-request-panel',
  imports: [RequestEditor, ResponseViewer],
  templateUrl: './request-panel.html',
  styleUrl: './request-panel.scss',
  host: {
    class: 'sidebar sidebar-right',
    'aria-label': 'Request and response panel',
  },
})
export class RequestPanel {
  protected readonly state = inject(WorkspaceStateService);
  private readonly requestApi = inject(RequestApiService);

  protected readonly responses = this.requestApi.responses;

  constructor() {
    effect(() => {
      const req = this.state.selectedRequest();
      if (req) {
        this.state.loadResponses(req.id);
      } else {
        this.requestApi.responses.set([]);
        this.state.selectedResponse.set(null);
        this.state.rightPanelMode.set('response');
      }
    });
  }

  setRightPanelMode(mode: RightPanelMode): void {
    this.state.rightPanelMode.set(mode);
  }
}
