import { ChangeDetectionStrategy, Component, computed, effect, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { type HttpRequest } from '../core/services/request.service';
import { ApiRequestsService } from '../core/services/api-requests.service';
import { EnvironmentVariableApiService } from '../core/services/environment-variable.service';
import { RequestBodyEditor } from '../request-body-editor/request-body-editor';
import { type BodyType } from '../body-editor/body-editor';
import { RequestDrawerTabBase } from '../request-bottom-drawer/request-drawer-tab.base';

interface BodyValue {
  body: string;
  bodyType: BodyType;
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
export class RequestBodyTab extends RequestDrawerTabBase<BodyValue> {
  private readonly apiRequests = inject(ApiRequestsService);
  private readonly envVarApi = inject(EnvironmentVariableApiService);

  readonly bodyTabState = this.tabState;
  readonly bodyEditCount = this.editCount;
  readonly bodySaving = this.saving;

  readonly bodyDraft = signal('');
  readonly bodyBaseline = signal('');
  readonly bodyType = signal<BodyType>('json');
  readonly bodyTypeBaseline = signal<BodyType>('json');
  readonly bodyWipeConfirmOpen = signal(false);
  readonly bodyWipeCount = signal(0);

  protected readonly environmentVariables = computed(() =>
    this.envVarApi.variables().map((v) => ({ key: v.key, value: v.value })),
  );

  readonly bodyDirty = computed(
    () => this.bodyDraft() !== this.bodyBaseline() || this.bodyType() !== this.bodyTypeBaseline(),
  );

  private bodySaveTimer: number | null = null;

  constructor() {
    super();

    effect(() => {
      const env = this.state.selectedEnvironment();
      if (env) {
        void this.envVarApi.loadForEnvironment(env.id);
      } else {
        this.envVarApi.variables.set([]);
      }
    });
  }

  protected override extractValue(req: HttpRequest): BodyValue {
    return { body: req.body, bodyType: req.body_type === 'text' ? 'text' : 'json' };
  }

  protected override valueEquals(a: BodyValue, b: BodyValue): boolean {
    return a.body === b.body && a.bodyType === b.bodyType;
  }

  protected override async saveFor(requests: HttpRequest[], value: BodyValue): Promise<HttpRequest[]> {
    const ids = requests.map((req) => req.id);
    return this.apiRequests.bulkUpdateBody(ids, value.body, value.bodyType);
  }

  protected override beforeReload(): void {
    if (this.bodySaveTimer !== null) {
      window.clearTimeout(this.bodySaveTimer);
      this.bodySaveTimer = null;
    }
    const single = this.requests().length === 1 ? this.requests()[0] : null;
    if (single && this.bodyDirty()) {
      void this.flush({ body: this.bodyDraft(), bodyType: this.bodyType() });
    }
  }

  protected override onLoaded(_requests: HttpRequest[], value: BodyValue | null): void {
    if (!value) return;
    this.bodyBaseline.set(value.body);
    this.bodyDraft.set(value.body);
    this.bodyTypeBaseline.set(value.bodyType);
    this.bodyType.set(value.bodyType);
  }

  setBodyType(type: BodyType): void {
    this.bodyType.set(type);
    if (this.editCount() === 1) {
      this.scheduleBodyAutosave();
    }
  }

  onBodyTextChange(text: string): void {
    this.bodyDraft.set(text);
    if (this.editCount() === 1) {
      this.scheduleBodyAutosave();
    }
  }

  private scheduleBodyAutosave(): void {
    if (this.bodySaveTimer !== null) window.clearTimeout(this.bodySaveTimer);
    this.bodySaveTimer = window.setTimeout(() => {
      this.bodySaveTimer = null;
      void this.commitBody();
    }, 400);
  }

  async applyBodyEdits(): Promise<void> {
    if (this.bodySaveTimer !== null) {
      window.clearTimeout(this.bodySaveTimer);
      this.bodySaveTimer = null;
    }
    await this.commitBody();
  }

  private async commitBody(): Promise<void> {
    const body = this.bodyDraft();
    const bodyType = this.bodyType();
    const saved = await this.save({ body, bodyType });
    if (saved.length > 0) {
      this.bodyBaseline.set(body);
      this.bodyTypeBaseline.set(bodyType);
    }
  }

  openBodyWipeConfirm(): void {
    const ids = this.state.selectedIds();
    if (ids.size === 0) return;
    this.bodyWipeCount.set(ids.size);
    this.bodyWipeConfirmOpen.set(true);
  }

  closeBodyWipeConfirm(): void {
    this.bodyWipeConfirmOpen.set(false);
  }

  async confirmClearAllSelectedBodies(): Promise<void> {
    if (this.requests().length === 0) {
      this.closeBodyWipeConfirm();
      return;
    }

    const value: BodyValue = { body: '', bodyType: 'json' };
    await this.save(value);
    this.bodyBaseline.set(value.body);
    this.bodyDraft.set(value.body);
    this.bodyTypeBaseline.set(value.bodyType);
    this.bodyType.set(value.bodyType);
    this.closeBodyWipeConfirm();
  }
}
