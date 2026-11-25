import { Component, DestroyRef, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { NgTemplateOutlet } from '@angular/common';
import { Clipboard, Dialogs } from '@wailsio/runtime';
import { DisplayService } from '../../../bindings/snap-rq/backend/mcp';
import type { MaskedResponse } from '../../../bindings/snap-rq/backend/mcp/models';
import { WorkspaceStateService } from '../core/services/workspace-state.service';
import { RequestApiService, type HttpRequest, type HttpResponse } from '../core/services/request.service';
import {
  detectResponseLanguage,
  prettifyResponseBody,
} from '../core/prettifiers/response-prettifier';
import { csvColumnWidths } from '../core/prettifiers/csv-prettifier';
import type { SyntaxLanguage } from '../core/highlighters/highlight';
import { httpStatusClass, httpStatusText, isRequestError } from '../core/http-status';
import { ScriptLog } from '../script-log/script-log';
import { ResponseHistory } from '../response-history/response-history';
import { RequestInFlight } from '../request-in-flight/request-in-flight';
import { ConfirmService } from '../core/services/confirm.service';
import { ResponseText } from '../response-text/response-text';

/**
 * Against flicker: a send that ends within SHOW_AFTER_MS never shows the
 * in-flight view, and once shown it stays SHOW_AT_LEAST_MS.
 */
const SHOW_AFTER_MS = 200;
const SHOW_AT_LEAST_MS = 500;

/** How a hidden cookie value is shown, as the backend shows other secrets. */
const HIDDEN = '•••••';

type ResponseSection = 'scripts' | 'headers' | 'cookies' | 'body';
type CopyTarget = 'all' | 'headers' | 'body';

@Component({
  selector: 'app-response-viewer',
  imports: [NgTemplateOutlet, ScriptLog, ResponseHistory, RequestInFlight, ResponseText],
  templateUrl: './response-viewer.html',
  styleUrl: './response-viewer.scss',
})
export class ResponseViewer {
  protected readonly state = inject(WorkspaceStateService);
  private readonly requestApi = inject(RequestApiService);
  private readonly confirm = inject(ConfirmService);

  /** Offers the bin that clears the request's history. */
  readonly canClearHistory = input(false);

  protected readonly responses = this.requestApi.responses;
  protected readonly statusClass = httpStatusClass;
  protected readonly statusText = httpStatusText;
  protected readonly isRequestError = isRequestError;
  protected readonly prettifyEnabled = signal(true);
  protected readonly showPrettifyToggle = false;

  /** The selected response when it is a request that failed (no response arrived). */
  protected readonly failedResponse = computed(() => {
    const response = this.state.selectedResponse();
    return response && isRequestError(response.status_code) ? response : null;
  });

  /** A failed request's message, without the backend's "request failed: " (the title says it). */
  protected readonly errorMessage = computed(() =>
    (this.failedResponse()?.body ?? '').replace(/^request failed:\s*/, ''),
  );

  /** The selected response with its secrets hidden; null until the backend has masked it. */
  private readonly masked = signal<{ id: number; view: MaskedResponse } | null>(null);
  private readonly maskedView = computed(() => {
    const resp = this.state.selectedResponse();
    const masked = this.masked();
    return resp && masked?.id === resp.id ? masked.view : null;
  });
  protected readonly headersMasked = computed(() => this.maskedView()?.headers_masked ?? false);
  protected readonly bodyMasked = computed(() => this.maskedView()?.body_masked ?? false);
  protected readonly urlMasked = computed(() => this.maskedView()?.url_masked ?? false);
  /** Per section, for the response shown; any other response starts hidden again. */
  protected readonly revealUrl = signal(false);
  protected readonly revealHeaders = signal(false);
  protected readonly revealBody = signal(false);
  protected readonly copied = signal<CopyTarget | null>(null);
  /** Kept while switching responses, so a folded section stays folded. */
  private readonly folded = signal<ReadonlySet<ResponseSection>>(new Set());
  private copiedTimer: ReturnType<typeof setTimeout> | undefined;

  protected readonly shownHeaders = computed(() => {
    const resp = this.state.selectedResponse();
    if (!resp) return '';
    return this.revealHeaders() ? resp.headers : (this.maskedView()?.headers ?? '');
  });
  protected readonly shownUrl = computed(() => {
    const resp = this.state.selectedResponse();
    if (!resp) return '';
    return this.revealUrl() ? resp.url : (this.maskedView()?.url ?? '');
  });
  private readonly shownBody = computed(() => {
    const resp = this.state.selectedResponse();
    if (!resp) return '';
    return this.revealBody() ? resp.body : (this.maskedView()?.body ?? '');
  });

  /** Set-Cookie lines of the selected response, parsed for display. */
  protected readonly responseCookies = computed(() => {
    const headers = this.state.selectedResponse()?.headers ?? '';
    const hide = this.headersMasked() && !this.revealHeaders();
    const out: { name: string; value: string; attrs: string }[] = [];
    for (const line of headers.split(/\r?\n/)) {
      const m = /^set-cookie:\s*(.*)$/i.exec(line);
      if (!m) continue;
      const [pair, ...rest] = m[1].split(';');
      const eq = pair.indexOf('=');
      out.push({
        name: (eq < 0 ? '' : pair.slice(0, eq)).trim(),
        value: hide ? HIDDEN : (eq < 0 ? pair : pair.slice(eq + 1)).trim(),
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
    effect(() => {
      const resp = this.state.selectedResponse();
      untracked(() => this.mask(resp));
    });
    inject(DestroyRef).onDestroy(() => {
      clearTimeout(this.inFlightTimer);
      clearTimeout(this.copiedTimer);
    });
  }

  private mask(resp: HttpResponse | null): void {
    this.revealUrl.set(false);
    this.revealHeaders.set(false);
    this.revealBody.set(false);
    if (!resp) {
      this.masked.set(null);
      return;
    }
    DisplayService.MaskResponse(resp.url ?? '', resp.headers ?? '', resp.body ?? '')
      .then((view) => {
        if (this.state.selectedResponse()?.id === resp.id) this.masked.set({ id: resp.id, view });
      })
      .catch((err: unknown) => console.error('Masking the response failed:', err));
  }

  /** Copies what is shown: hidden values stay hidden unless revealed. */
  async copy(target: CopyTarget): Promise<void> {
    const text =
      target === 'headers' ? this.shownHeaders() : target === 'body' ? this.bodyText() : this.wholeResponse();
    try {
      await Clipboard.SetText(text);
      this.copied.set(target);
      clearTimeout(this.copiedTimer);
      this.copiedTimer = setTimeout(() => this.copied.set(null), 1500);
    } catch (err) {
      console.error('Copying failed:', err);
    }
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

  private wholeResponse(): string {
    const resp = this.state.selectedResponse();
    if (!resp) return '';
    const status = [resp.status_code, this.statusText(resp.status_code)].filter(Boolean).join(' ');
    const parts = [status];
    if (resp.url) parts.push(`${resp.method} ${this.shownUrl()}`);
    parts.push('', this.shownHeaders(), '', this.bodyText());
    return parts.join('\n');
  }

  isFolded(section: ResponseSection): boolean {
    return this.folded().has(section);
  }

  toggleFold(section: ResponseSection): void {
    this.folded.update((current) => {
      const next = new Set(current);
      if (!next.delete(section)) next.add(section);
      return next;
    });
  }

  protected readonly bodyText = computed(() => {
    const resp = this.state.selectedResponse();
    const body = this.shownBody();
    if (!resp || !body) return '';
    return prettifyResponseBody(body, resp.headers, this.prettifyEnabled());
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

  selectResponse(resp: HttpResponse): void {
    this.state.selectedResponse.set(resp);
  }

  async clearHistory(): Promise<void> {
    const requestId = this.responses()[0]?.request_id;
    if (requestId === undefined) return;
    const count = this.responses().length;
    const name = this.state.selectedRequest()?.name;
    const saved = count === 1 ? 'The saved response' : `All ${count} saved responses`;
    const confirmed = await this.confirm.ask({
      title: 'Clear history?',
      message: `${saved}${name ? ` of "${name}"` : ''} will be permanently deleted. This cannot be undone.`,
      confirmLabel: 'Clear history',
      danger: true,
    });
    if (!confirmed) return;
    try {
      await this.requestApi.clearResponses(requestId);
      this.state.selectedResponse.set(null);
    } catch (err) {
      console.error(err);
    }
  }

  togglePrettify(): void {
    this.prettifyEnabled.update(enabled => !enabled);
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
