import { Component, computed, inject, signal } from '@angular/core';
import { CdkFixedSizeVirtualScroll, CdkVirtualForOf, CdkVirtualScrollViewport } from '@angular/cdk/scrolling';
import { Dialogs } from '@wailsio/runtime';
import { WorkspaceStateService } from '../core/services/workspace-state.service';
import { RequestApiService, type HttpResponse } from '../core/services/request.service';
import { prettifyResponseBody } from '../core/prettifiers/response-prettifier';

@Component({
  selector: 'app-response-viewer',
  imports: [CdkVirtualScrollViewport, CdkVirtualForOf, CdkFixedSizeVirtualScroll],
  templateUrl: './response-viewer.html',
  styleUrl: './response-viewer.scss',
})
export class ResponseViewer {
  protected readonly state = inject(WorkspaceStateService);
  private readonly requestApi = inject(RequestApiService);

  protected readonly responses = this.requestApi.responses;
  protected readonly prettifyEnabled = signal(true);

  readonly bodyLines = computed<string[]>(() => {
    const resp = this.state.selectedResponse();
    if (!resp || !resp.body || resp.body.length === 0) {
      return [];
    }
    const formatted = prettifyResponseBody(resp.body, resp.headers, this.prettifyEnabled());
    return formatted.length > 0 ? formatted.split('\n') : [];
  });

  trackByLine(index: number, line: string): number {
    return index;
  }

  selectResponse(resp: HttpResponse): void {
    this.state.selectedResponse.set(resp);
  }

  togglePrettify(): void {
    this.prettifyEnabled.update(enabled => !enabled);
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
