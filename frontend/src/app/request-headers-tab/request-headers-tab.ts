import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { type HttpRequest } from '../core/services/request.service';
import { ApiRequestsService } from '../core/services/api-requests.service';
import { RequestHeadersEditor } from '../request-headers-editor/request-headers-editor';
import { RequestCodeTabBase } from '../request-bottom-drawer/request-code-tab.base';

/**
 * Headers tab content for the request bottom drawer.
 *
 * Loads headers for the current selection, allows editing when they match, and
 * offers a bulk clear when they don't.
 */
@Component({
  selector: 'app-request-headers-tab',
  imports: [FormsModule, RequestHeadersEditor],
  templateUrl: './request-headers-tab.html',
  styleUrl: './request-headers-tab.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class RequestHeadersTab extends RequestCodeTabBase<string> {
  private readonly apiRequests = inject(ApiRequestsService);

  readonly headersTabState = this.tabState;
  readonly headersEditCount = this.editCount;
  readonly headersSaving = this.saving;

  readonly headersDraft = computed(() => this.draftValue() ?? '');
  readonly headersDirty = this.dirty;
  readonly headersWipeConfirmOpen = this.wipeConfirmOpen;
  readonly headersWipeCount = this.wipeCount;

  protected override extractValue(req: HttpRequest): string {
    return req.request_headers;
  }

  protected override valueEquals(a: string, b: string): boolean {
    return a === b;
  }

  protected override saveFor(requests: HttpRequest[], value: string): Promise<HttpRequest[]> {
    const ids = requests.map((req) => req.id);
    return this.apiRequests.bulkUpdateHeaders(ids, value);
  }

  protected override emptyValue(): string {
    return '';
  }

  onHeadersChange(headers: string): void {
    this.updateDraft(headers);
  }

  applyHeadersEdits(): Promise<void> {
    return this.applyDraft();
  }

  openHeadersWipeConfirm(): void {
    this.openWipeConfirm();
  }

  closeHeadersWipeConfirm(): void {
    this.closeWipeConfirm();
  }

  confirmClearAllSelectedHeaders(): Promise<void> {
    return this.confirmClearAll();
  }
}
