import { Component, inject } from '@angular/core';
import { Dialogs } from '@wailsio/runtime';
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

  formatTime(createdAt: string): string {
    const utc = createdAt.replace(' ', 'T') + 'Z';
    const date = new Date(utc);
    if (Number.isNaN(date.getTime())) {
      return createdAt;
    }
    return date.toLocaleString(undefined, {
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    });
  }

  async downloadResponse(): Promise<void> {
    const resp = this.state.selectedResponse();
    if (!resp) return;

    const defaultName = `response-${resp.id}.txt`;
    try {
      const filePath = await Dialogs.SaveFile({
        Filename: defaultName,
        Title: 'Download response',
        ButtonText: 'Save',
      });
      if (!filePath) return;

      await this.requestApi.saveResponseToFile(resp.id, filePath);
    } catch (err) {
      console.error('Failed to download response:', err);
    }
  }
}
