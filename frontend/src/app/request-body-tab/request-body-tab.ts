import {
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  inject,
  input,
  signal,
  untracked,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { WorkspaceStateService } from '../core/services/workspace-state.service';
import { RequestApiService, type HttpRequest } from '../core/services/request.service';
import { ApiRequestsService } from '../core/services/api-requests.service';
import { FavouriteApiService } from '../core/services/favourite.service';
import { EnvironmentVariableApiService } from '../core/services/environment-variable.service';
import { RequestBodyEditor } from '../request-body-editor/request-body-editor';
import { type BodyType } from '../body-editor/body-editor';

type BodyTabState = 'idle' | 'loading' | 'ready' | 'mismatch' | 'unavailable';

/** Treats empty or parseable JSON as JSON so the editor offers formatting. */
function looksLikeJson(text: string): boolean {
  const trimmed = text.trim();
  if (!trimmed) return true;
  try {
    JSON.parse(trimmed);
    return true;
  } catch {
    return false;
  }
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
export class RequestBodyTab {
  private readonly state = inject(WorkspaceStateService);
  private readonly requestApi = inject(RequestApiService);
  private readonly apiRequests = inject(ApiRequestsService);
  private readonly favouriteApi = inject(FavouriteApiService);
  private readonly envVarApi = inject(EnvironmentVariableApiService);

  readonly selectedIds = input<Set<number>>(new Set());

  readonly bodyTabState = signal<BodyTabState>('idle');
  readonly bodyDraft = signal('');
  readonly bodyBaseline = signal('');
  readonly bodyType = signal<BodyType>('json');
  readonly bodySaving = signal(false);
  readonly bodyEditCount = signal(0);
  readonly bodyWipeConfirmOpen = signal(false);
  readonly bodyWipeCount = signal(0);

  protected readonly environmentVariables = computed(() =>
    this.envVarApi.variables().map((v) => ({ key: v.key, value: v.value })),
  );

  protected readonly bodyEditorType = computed<BodyType>(() => this.bodyType());

  readonly bodyDirty = computed(() => this.bodyDraft() !== this.bodyBaseline());

  private bodyRequests: HttpRequest[] = [];
  private bodyLoadVersion = 0;
  private bodySaveTimer: number | null = null;

  constructor() {
    effect(() => {
      const ids = this.selectedIds();
      untracked(() => void this.refreshBodyForSelection(ids));
    });

    effect(() => {
      const env = this.state.selectedEnvironment();
      if (env) {
        void this.envVarApi.loadForEnvironment(env.id);
      } else {
        this.envVarApi.variables.set([]);
      }
    });
  }

  private async refreshBodyForSelection(ids: Set<number>): Promise<void> {
    const version = ++this.bodyLoadVersion;

    this.flushPendingBodyAutosave();
    this.bodyRequests = [];
    this.bodyEditCount.set(0);

    if (ids.size === 0) {
      this.bodyTabState.set('idle');
      return;
    }

    this.bodyTabState.set('loading');
    try {
      const requests = await Promise.all([...ids].map((id) => this.requestApi.get(id)));
      if (version !== this.bodyLoadVersion) return;

      const first = requests[0]?.body ?? '';
      if (!requests.every((r) => r.body === first)) {
        this.bodyTabState.set('mismatch');
        return;
      }

      this.bodyRequests = requests;
      this.bodyBaseline.set(first);
      this.bodyDraft.set(first);
      this.bodyType.set(looksLikeJson(first) ? 'json' : 'text');
      this.bodyEditCount.set(requests.length);
      this.bodyTabState.set('ready');
    } catch (err) {
      console.error(err);
      if (version === this.bodyLoadVersion) {
        this.bodyTabState.set('unavailable');
      }
    }
  }

  setBodyType(type: BodyType): void {
    this.bodyType.set(type);
  }

  onBodyTextChange(text: string): void {
    this.bodyDraft.set(text);
    if (this.bodyEditCount() === 1) {
      this.scheduleBodyAutosave();
    }
  }

  private scheduleBodyAutosave(): void {
    if (this.bodySaveTimer !== null) window.clearTimeout(this.bodySaveTimer);
    this.bodySaveTimer = window.setTimeout(() => {
      this.bodySaveTimer = null;
      void this.commitBody(this.bodyDraft());
    }, 400);
  }

  async applyBodyEdits(): Promise<void> {
    if (this.bodySaveTimer !== null) {
      window.clearTimeout(this.bodySaveTimer);
      this.bodySaveTimer = null;
    }
    await this.commitBody(this.bodyDraft());
  }

  private async commitBody(body: string): Promise<void> {
    const requests = this.bodyRequests;
    if (requests.length === 0) return;

    this.bodySaving.set(true);
    try {
      const saved = await this.saveBodyFor(requests, body);
      this.bodyRequests = saved;
      this.bodyBaseline.set(body);
    } catch (err) {
      console.error(err);
    } finally {
      this.bodySaving.set(false);
    }
  }

  openBodyWipeConfirm(): void {
    const ids = [...this.selectedIds()];
    if (ids.length === 0) return;
    this.bodyWipeCount.set(ids.length);
    this.bodyWipeConfirmOpen.set(true);
  }

  closeBodyWipeConfirm(): void {
    this.bodyWipeConfirmOpen.set(false);
  }

  async confirmClearAllSelectedBodies(): Promise<void> {
    const ids = [...this.selectedIds()];
    if (ids.length === 0) {
      this.closeBodyWipeConfirm();
      return;
    }

    this.bodySaving.set(true);
    try {
      const saved = await this.apiRequests.bulkUpdateBody(ids, '');
      this.syncRequestsInWorkspace(saved);

      this.bodyRequests = saved;
      this.bodyBaseline.set('');
      this.bodyDraft.set('');
      this.bodyType.set('json');
      this.bodyEditCount.set(saved.length);
      this.bodyTabState.set('ready');
    } catch (err) {
      console.error(err);
    } finally {
      this.bodySaving.set(false);
      this.closeBodyWipeConfirm();
    }
  }

  /**
   * Persists one body value onto every given request and refreshes the shared
   * selection/favourite copies. Does not touch the tab's own request list,
   * so callers can flush without racing a selection change.
   */
  private async saveBodyFor(requests: HttpRequest[], body: string): Promise<HttpRequest[]> {
    const ids = requests.map((req) => req.id);
    const saved = await this.apiRequests.bulkUpdateBody(ids, body);
    this.syncRequestsInWorkspace(saved);
    return saved;
  }

  private flushPendingBodyAutosave(): void {
    if (this.bodySaveTimer !== null) {
      window.clearTimeout(this.bodySaveTimer);
      this.bodySaveTimer = null;
    }
    const single = this.bodyRequests.length === 1 ? this.bodyRequests[0] : null;
    if (single && this.bodyDirty()) {
      void this.saveBodyFor([single], this.bodyDraft());
    }
  }

  private syncRequestsInWorkspace(saved: HttpRequest[]): void {
    for (const updated of saved) {
      if (this.state.selectedRequest()?.id === updated.id) {
        this.state.selectedRequest.set(updated);
      }
      const favList = this.favouriteApi.requests();
      const favIndex = favList.findIndex((r) => r.id === updated.id);
      if (favIndex !== -1) {
        const newFavList = [...favList];
        newFavList[favIndex] = updated;
        this.favouriteApi.requests.set(newFavList);
      }
    }
  }
}
