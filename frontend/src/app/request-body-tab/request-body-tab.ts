import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { type HttpRequest } from '../core/services/request.service';
import { ApiRequestsService } from '../core/services/api-requests.service';
import { RequestBodyEditor } from '../request-body-editor/request-body-editor';
import { type EditorLanguage } from '../code-editor/code-editor';
import { RequestCodeTabBase } from '../request-bottom-drawer/request-code-tab.base';

interface BodyValue {
  body: string;
  bodyType: EditorLanguage;
}

/**
 * Body tab content for the request bottom drawer.
 *
 * Loads bodies for the current selection, allows editing when they match, and
 * offers a bulk clear when they don't.
 */
@Component({
  selector: 'app-request-body-tab',
  imports: [FormsModule, RequestBodyEditor],
  templateUrl: './request-body-tab.html',
  styleUrl: './request-body-tab.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class RequestBodyTab extends RequestCodeTabBase<BodyValue> {
  private readonly apiRequests = inject(ApiRequestsService);

  readonly bodyTabState = this.tabState;
  readonly bodyEditCount = this.editCount;
  readonly bodySaving = this.saving;

  readonly bodyDraft = computed(() => this.draftValue()?.body ?? '');
  readonly bodyType = computed<EditorLanguage>(() => this.draftValue()?.bodyType ?? 'json');
  readonly bodyDirty = this.dirty;
  readonly bodyWipeConfirmOpen = this.wipeConfirmOpen;
  readonly bodyWipeCount = this.wipeCount;

  protected override extractValue(req: HttpRequest): BodyValue {
    return { body: req.body, bodyType: req.body_type === 'text' ? 'text' : 'json' };
  }

  protected override valueEquals(a: BodyValue, b: BodyValue): boolean {
    return a.body === b.body && a.bodyType === b.bodyType;
  }

  protected override saveFor(requests: HttpRequest[], value: BodyValue): Promise<HttpRequest[]> {
    const ids = requests.map((req) => req.id);
    return this.apiRequests.bulkUpdateBody(ids, value.body, value.bodyType);
  }

  protected override emptyValue(): BodyValue {
    return { body: '', bodyType: 'json' };
  }

  setBodyType(type: EditorLanguage): void {
    const draft = this.draftValue();
    if (!draft) return;
    this.updateDraft({ ...draft, bodyType: type });
  }

  onBodyTextChange(text: string): void {
    const draft = this.draftValue();
    if (!draft) return;
    this.updateDraft({ ...draft, body: text });
  }

  applyBodyEdits(): Promise<void> {
    return this.applyDraft();
  }

  openBodyWipeConfirm(): void {
    this.openWipeConfirm();
  }

  closeBodyWipeConfirm(): void {
    this.closeWipeConfirm();
  }

  confirmClearAllSelectedBodies(): Promise<void> {
    return this.confirmClearAll();
  }
}
