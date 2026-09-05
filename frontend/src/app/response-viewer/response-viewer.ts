import { Component, inject } from '@angular/core';
import { WorkspaceStateService } from '../core/services/workspace-state.service';
import { RequestApiService, type HttpResponse } from '../core/services/request.service';

@Component({
  selector: 'app-response-viewer',
  templateUrl: './response-viewer.html',
  styleUrl: './response-viewer.scss',
})
export class ResponseViewer {
  protected readonly state = inject(WorkspaceStateService);
  private readonly requestApi = inject(RequestApiService);

  protected readonly responses = this.requestApi.responses;

  selectResponse(resp: HttpResponse): void {
    this.state.selectedResponse.set(resp);
  }
}
