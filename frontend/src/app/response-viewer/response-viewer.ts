import { Component, DestroyRef, computed, effect, inject, signal, untracked } from '@angular/core';
import { CdkFixedSizeVirtualScroll, CdkVirtualForOf, CdkVirtualScrollViewport } from '@angular/cdk/scrolling';
import { Dialogs } from '@wailsio/runtime';
import { WorkspaceStateService } from '../core/services/workspace-state.service';
import { RequestApiService, type HttpRequest, type HttpResponse } from '../core/services/request.service';
import {
  detectResponseLanguage,
  prettifyResponseBody,
} from '../core/prettifiers/response-prettifier';
import { csvColumnWidths } from '../core/prettifiers/csv-prettifier';
import { highlightLine, type SyntaxLanguage, type SyntaxToken } from '../core/highlighters/highlight';
import { httpStatusClass, httpStatusText, isRequestError } from '../core/http-status';
import { ScriptLog } from '../script-log/script-log';
import { ResponseHistory } from '../response-history/response-history';
import { RequestInFlight } from '../request-in-flight/request-in-flight';

/**
 * Against flicker: a send that ends within SHOW_AFTER_MS never shows the
 * in-flight view, and once shown it stays SHOW_AT_LEAST_MS.
 */
const SHOW_AFTER_MS = 200;
const SHOW_AT_LEAST_MS = 500;

@Component({
  selector: 'app-response-viewer',
  imports: [CdkVirtualScrollViewport, CdkVirtualForOf, CdkFixedSizeVirtualScroll, ScriptLog, ResponseHistory, RequestInFlight],
  templateUrl: './response-viewer.html',
  styleUrl: './response-viewer.scss',
})
export class ResponseViewer {
  protected readonly state = inject(WorkspaceStateService);
  private readonly requestApi = inject(RequestApiService);

  protected readonly responses = this.requestApi.responses;
  protected readonly statusClass = httpStatusClass;
  protected readonly statusText = httpStatusText;
  protected readonly isRequestError = isRequestError;
  protected readonly prettifyEnabled = signal(true);

  /** The selected response when it is a request that failed (no response arrived). */
  protected readonly failedResponse = computed(() => {
    const response = this.state.selectedResponse();
    return response && isRequestError(response.status_code) ? response : null;
  });

  /** A failed request's message, without the backend's "request failed: " (the title says it). */
  protected readonly errorMessage = computed(() =>
    (this.failedResponse()?.body ?? '').replace(/^request failed:\s*/, ''),
  );

  /** Set-Cookie lines of the selected response, parsed for display. */
  protected readonly responseCookies = computed(() => {
    const headers = this.state.selectedResponse()?.headers ?? '';
    const out: { name: string; value: string; attrs: string }[] = [];
    for (const line of headers.split(/\r?\n/)) {
      const m = /^set-cookie:\s*(.*)$/i.exec(line);
      if (!m) continue;
      const [pair, ...rest] = m[1].split(';');
      const eq = pair.indexOf('=');
      out.push({
        name: (eq < 0 ? '' : pair.slice(0, eq)).trim(),
        value: (eq < 0 ? pair : pair.slice(eq + 1)).trim(),
        attrs: rest.map((a) => a.trim()).filter(Boolean).join('; '),
      });
    }
    return out;
  });

  private readonly inFlight = computed(() => {
    const request = this.state.selectedRequest();
    return request && this.requestApi.sending()[request.id] !== undefined ? request : null;
  });

  /** The request whose in-flight view is shown instead of its responses. */
  protected readonly shownInFlight = signal<HttpRequest | null>(null);
  private shownAt = 0;
  private inFlightTimer: ReturnType<typeof setTimeout> | undefined;

  constructor() {
    effect(() => {
      const request = this.inFlight();
      untracked(() => this.updateInFlightView(request));
    });
    inject(DestroyRef).onDestroy(() => clearTimeout(this.inFlightTimer));
  }

  private updateInFlightView(request: HttpRequest | null): void {
    clearTimeout(this.inFlightTimer);
    const shown = this.shownInFlight();

    if (request) {
      // Measured from the send, so a request already running shows at once.
      const startedAt = this.requestApi.sending()[request.id] ?? Date.now();
      const wait = SHOW_AFTER_MS - (Date.now() - startedAt);
      if (shown?.id === request.id || wait <= 0) {
        this.showInFlight(request);
        return;
      }
      this.shownInFlight.set(null);
      this.inFlightTimer = setTimeout(() => this.showInFlight(request), wait);
      return;
    }

    if (!shown) return;
    const left = SHOW_AT_LEAST_MS - (Date.now() - this.shownAt);
    if (shown.id !== this.state.selectedRequest()?.id || left <= 0) {
      this.shownInFlight.set(null);
      return;
    }
    this.inFlightTimer = setTimeout(() => this.shownInFlight.set(null), left);
  }

  private showInFlight(request: HttpRequest): void {
    if (this.shownInFlight()?.id !== request.id) this.shownAt = Date.now();
    this.shownInFlight.set(request);
  }

  readonly bodyLines = computed<string[]>(() => {
    const resp = this.state.selectedResponse();
    if (!resp || !resp.body || resp.body.length === 0) {
      return [];
    }
    const formatted = prettifyResponseBody(resp.body, resp.headers, this.prettifyEnabled());
    return formatted.length > 0 ? formatted.split('\n') : [];
  });

  readonly bodyLanguage = computed<SyntaxLanguage>(() => {
    const resp = this.state.selectedResponse();
    if (!resp || !resp.body || resp.body.length === 0) {
      return 'text';
    }
    return detectResponseLanguage(resp.body, resp.headers);
  });

  readonly bodyColumnWidths = computed<number[]>(() => {
    if (this.bodyLanguage() !== 'csv' || !this.prettifyEnabled()) {
      return [];
    }
    const resp = this.state.selectedResponse();
    return resp?.body ? csvColumnWidths(resp.body) : [];
  });

  lineTokens(line: string): SyntaxToken[] {
    return highlightLine(line, this.bodyLanguage(), this.bodyColumnWidths());
  }

  tokenClass(type: SyntaxToken['type']): string {
    return type === 'plain' ? '' : `tok-${type}`;
  }

  trackByLine(index: number, line: string): number {
    return index;
  }

  selectResponse(resp: HttpResponse): void {
    this.state.selectedResponse.set(resp);
  }

  togglePrettify(): void {
    this.prettifyEnabled.update(enabled => !enabled);
  }

  onBlockKeyDown(event: KeyboardEvent): void {
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'a') {
      event.preventDefault();
      const block = event.currentTarget as HTMLElement | null;
      if (!block) return;
      const selection = window.getSelection();
      if (!selection) return;
      const range = document.createRange();
      range.selectNodeContents(block);
      selection.removeAllRanges();
      selection.addRange(range);
    }
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
