import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { RequestApiService, type HttpRequest } from '../core/services/request.service';
import { FormsModule } from '@angular/forms';
import { RequestBodyEditor, type BodyKind } from '../request-body-editor/request-body-editor';
import { RequestCodeTabBase } from '../request-bottom-drawer/request-code-tab.base';

type BodyType = 'json' | 'text';

interface BodyValue {
  body: string;
  bodyType: BodyType;
  bodyForm: string;
  bodyMode: BodyKind;
}

function deriveBodyMode(req: HttpRequest): BodyKind {
  if (req.body_mode === 'json' || req.body_mode === 'text' || req.body_mode === 'form-data') {
    return req.body_mode;
  }
  if (req.body_form) return 'form-data';
  return req.body_type === 'text' ? 'text' : 'json';
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
    readonly bodyTabState = this.tabState;
  readonly bodySaving = this.saving;

  readonly bodyDraft = computed(() => this.draftValue()?.body ?? '');
  readonly bodyFormDraft = computed(() => this.draftValue()?.bodyForm ?? '');
  readonly bodyType = computed<BodyKind>(() => this.draftValue()?.bodyMode ?? 'json');
  readonly bodyDirty = this.dirty;
  readonly bodyWipeConfirmOpen = this.wipeConfirmOpen;
  readonly bodyWipeCount = this.wipeCount;

  protected override extractValue(req: HttpRequest): BodyValue {
    return {
      body: req.body,
      bodyType: req.body_type === 'text' ? 'text' : 'json',
      bodyForm: req.body_form,
      bodyMode: deriveBodyMode(req),
    };
  }

  protected override valueEquals(a: BodyValue, b: BodyValue): boolean {
    return (
      a.body === b.body &&
      a.bodyType === b.bodyType &&
      a.bodyForm === b.bodyForm &&
      a.bodyMode === b.bodyMode
    );
  }

protected override async saveFor(requests: HttpRequest[], value: BodyValue): Promise<void> {
  // A single edit is a normal request update: not undoable
  // Only a real multi-selection edit is a bulk operation worth an undo step.
  if (requests.length === 1) {
    await this.requestApi.update({
      ...requests[0],
      body: value.body,
      body_type: value.bodyType,
      body_form: value.bodyForm,
      body_mode: value.bodyMode,
    });
    return;
  }
  const ids = requests.map((req) => req.id);
  await this.requestApi.bulkUpdateBody(ids, value.body, value.bodyType);
  await this.requestApi.bulkUpdateBodyForm(ids, value.bodyForm);
  await this.requestApi.bulkUpdateBodyMode(ids, value.bodyMode);
}

  protected override emptyValue(): BodyValue {
    return { body: '', bodyType: 'json', bodyForm: '', bodyMode: 'json' };
  }

  setBodyType(type: BodyKind): void {
    const draft = this.draftValue();
    if (!draft) return;
    if (type === 'form-data') {
      this.updateDraft({ ...draft, bodyMode: type, bodyForm: draft.bodyForm || '[]' });
    } else {
      this.updateDraft({ ...draft, bodyMode: type, bodyType: type });
    }
  }

  onBodyTextChange(text: string): void {
    const draft = this.draftValue();
    if (!draft) return;
    this.updateDraft({ ...draft, body: text });
  }

  onBodyFormChange(form: string): void {
    const draft = this.draftValue();
    if (!draft) return;
    this.updateDraft({ ...draft, bodyForm: form });
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
