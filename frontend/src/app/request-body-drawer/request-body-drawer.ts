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
import { WorkspaceStateService } from '../core/services/workspace-state.service';
import { RequestApiService, type HttpRequest } from '../core/services/request.service';
import { ApiRequestsService } from '../core/services/api-requests.service';
import { FavouriteApiService } from '../core/services/favourite.service';
import { EnvironmentVariableApiService } from '../core/services/environment-variable.service';
import { BodyEditor, type BodyType } from '../body-editor/body-editor';

type BodyDrawerState = 'idle' | 'loading' | 'ready' | 'mismatch' | 'unavailable';

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
 * Bottom Body tab + sliding editor for the request list.
 *
 * Owns everything related to viewing/editing request bodies for the current
 * selection, including the mismatch + bulk-clear flow.
 */
@Component({
  selector: 'app-request-body-drawer',
  imports: [BodyEditor],
  templateUrl: './request-body-drawer.html',
  styleUrl: './request-body-drawer.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: {
    '(window:keydown.escape)': 'onEscapePressed()',
  },
})
export class RequestBodyDrawer {
  private readonly state = inject(WorkspaceStateService);
  private readonly requestApi = inject(RequestApiService);
  private readonly apiRequests = inject(ApiRequestsService);
  private readonly favouriteApi = inject(FavouriteApiService);
  private readonly envVarApi = inject(EnvironmentVariableApiService);

  readonly selectedIds = input<Set<number>>(new Set());
  readonly showingBin = input<boolean>(false);

  readonly bodyDrawerOpen = signal(false);
  readonly bodyDrawerState = signal<BodyDrawerState>('idle');
  readonly bodyDraft = signal('');
  readonly bodyBaseline = signal('');
  readonly bodyIsJson = signal(true);
  readonly bodySaving = signal(false);
  readonly bodyEditCount = signal(0);
  readonly bodyWipeConfirmOpen = signal(false);
  readonly bodyWipeCount = signal(0);

  protected readonly environmentVariables = computed(() =>
    this.envVarApi.variables().map((v) => ({ key: v.key, value: v.value })),
  );

  protected readonly bodyEditorType = computed<BodyType>(() =>
    this.bodyIsJson() ? 'json' : 'text',
  );

  readonly bodyDirty = computed(() => this.bodyDraft() !== this.bodyBaseline());

  readonly bodyTabHint = computed<string>(() => {
    if (this.showingBin()) return 'Body editing is not available in the bin';
    const size = this.selectedIds().size;
    if (size === 0) return 'Select a request to edit its body';
    if (this.bodyDrawerState() === 'loading') return 'Loading selected bodies…';
    if (this.bodyDrawerState() === 'mismatch') return 'Selected requests have different bodies';
    if (this.bodyDrawerState() === 'unavailable') return 'Could not load request bodies';
    return 'Edit request body';
  });

  private bodyRequests: HttpRequest[] = [];
  private bodyLoadVersion = 0;
  private bodySaveTimer: number | null = null;

  constructor() {
    effect(() => {
      const ids = this.selectedIds();
      const showBin = this.showingBin();
      untracked(() => void this.refreshBodyForSelection(ids, showBin));
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

  onEscapePressed(): void {
    if (this.bodyWipeConfirmOpen()) {
      this.closeBodyWipeConfirm();
      return;
    }
    if (this.bodyDrawerOpen()) {
      this.minimiseBodyDrawer();
    }
  }

  private async refreshBodyForSelection(ids: Set<number>, showBin: boolean): Promise<void> {
    const version = ++this.bodyLoadVersion;

    // Flush a pending single-selection autosave before dropping the old
    // requests, so keystrokes are not lost when the selection changes.
    this.flushPendingBodyAutosave();
    this.bodyRequests = [];
    this.bodyEditCount.set(0);

    if (showBin || ids.size === 0) {
      this.bodyDrawerState.set('idle');
      return;
    }

    this.bodyDrawerState.set('loading');
    try {
      const requests = await Promise.all([...ids].map((id) => this.requestApi.get(id)));
      if (version !== this.bodyLoadVersion) return;

      const first = requests[0]?.body ?? '';
      if (!requests.every((r) => r.body === first)) {
        this.bodyDrawerState.set('mismatch');
        return;
      }

      this.bodyRequests = requests;
      this.bodyBaseline.set(first);
      this.bodyDraft.set(first);
      this.bodyIsJson.set(looksLikeJson(first));
      this.bodyEditCount.set(requests.length);
      this.bodyDrawerState.set('ready');
    } catch (err) {
      console.error(err);
      if (version === this.bodyLoadVersion) {
        this.bodyDrawerState.set('unavailable');
      }
    }
  }

  toggleBodyTab(): void {
    const opening = !this.bodyDrawerOpen();
    this.bodyDrawerOpen.set(opening);
    if (opening) {
      this.bodyDraft.set(this.bodyBaseline());
    }
  }

  minimiseBodyDrawer(): void {
    if (!this.bodyDrawerOpen()) return;
    this.closeBodyDrawer();
  }

  private flushPendingBodyAutosave(): void {
    if (this.bodySaveTimer !== null) {
      window.clearTimeout(this.bodySaveTimer);
      this.bodySaveTimer = null;
    }
    // Flush a pending single-selection autosave. Capture the request now so a
    // caller that resets `bodyRequests` immediately after does not race it.
    const single = this.bodyRequests.length === 1 ? this.bodyRequests[0] : null;
    if (single && this.bodyDirty()) {
      void this.saveBodyFor([single], this.bodyDraft());
    }
  }

  private closeBodyDrawer(): void {
    this.flushPendingBodyAutosave();
    this.bodyDrawerOpen.set(false);
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
      this.bodyIsJson.set(true);
      this.bodyEditCount.set(saved.length);
      this.bodyDrawerState.set('ready');
    } catch (err) {
      console.error(err);
    } finally {
      this.bodySaving.set(false);
      this.closeBodyWipeConfirm();
    }
  }

  /**
   * Persists one body value onto every given request and refreshes the shared
   * selection/favourite copies. Does not touch the drawer's own request list,
   * so callers can flush without racing a selection change.
   */
  private async saveBodyFor(requests: HttpRequest[], body: string): Promise<HttpRequest[]> {
    const ids = requests.map((req) => req.id);
    const saved = await this.apiRequests.bulkUpdateBody(ids, body);
    this.syncRequestsInWorkspace(saved);
    return saved;
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
