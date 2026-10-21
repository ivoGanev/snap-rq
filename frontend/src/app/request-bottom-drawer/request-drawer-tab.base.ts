import { effect, inject, signal, untracked } from '@angular/core';
import { WorkspaceStateService } from '../core/services/workspace-state.service';
import { RequestApiService, type HttpRequest } from '../core/services/request.service';
import { FavouriteApiService } from '../core/services/favourite.service';

export type DrawerTabState = 'idle' | 'loading' | 'ready' | 'mismatch' | 'unavailable';

/**
 * Shared behaviour for bottom-drawer tabs that edit one or more fields of the
 * currently selected request(s).
 *
 * It handles the parts every field tab has in common:
 *  - reading the current selection from {@link WorkspaceStateService}
 *  - loading the full selected requests
 *  - detecting whether the tab's value matches across the selection
 *  - reloading when the active request changes elsewhere (e.g. Zen mode)
 *  - persisting a value across the selection and refreshing shared copies
 *
 * Subclasses declare how to extract/compare their value and how to save it.
 */
export abstract class RequestDrawerTabBase<T> {
  protected readonly state = inject(WorkspaceStateService);
  protected readonly requestApi = inject(RequestApiService);
  private readonly favouriteApi = inject(FavouriteApiService);

  /** Current lifecycle state of the tab for the active selection. */
  readonly tabState = signal<DrawerTabState>('idle');
  /** The full requests backing the current selection. */
  readonly requests = signal<HttpRequest[]>([]);
  /** Number of requests currently being edited. */
  readonly editCount = signal(0);
  /** True while a save is in flight. */
  readonly saving = signal(false);

  private loadVersion = 0;

  constructor() {
    effect(() => {
      const ids = this.state.selectedIds();
      untracked(() => void this.load(ids));
    });

    // Reload when the active request is changed elsewhere (e.g. Zen mode) and
    // the loaded copy no longer matches.
    effect(() => {
      const active = this.state.selectedRequest();
      if (!active) return;

      const status = untracked(() => this.tabState());
      if (status === 'idle' || status === 'loading') return;

      const ids = untracked(() => this.state.selectedIds());
      if (!ids.has(active.id)) return;

      if (!untracked(() => this.isStale(active))) return;

      void this.load(ids);
    });
  }

  /** Extracts this tab's value from a request. */
  protected abstract extractValue(req: HttpRequest): T;

  /** Returns true when two values are considered equal. */
  protected abstract valueEquals(a: T, b: T): boolean;

  /** Persists `value` onto every given request and returns the saved rows. */
  protected abstract saveFor(requests: HttpRequest[], value: T): Promise<HttpRequest[]>;

  /** Called right before a (re)load, e.g. to flush pending autosaves. */
  protected beforeReload(): void {}

  /** Called after a successful load; `value` is null when the selection mismatches. */
  protected onLoaded(_requests: HttpRequest[], _value: T | null): void {}

  protected async load(ids: Set<number>): Promise<void> {
    const version = ++this.loadVersion;

    this.beforeReload();

    if (ids.size === 0) {
      this.requests.set([]);
      this.editCount.set(0);
      this.tabState.set('idle');
      return;
    }

    try {
      const requests = await Promise.all([...ids].map((id) => this.requestApi.get(id)));
      if (version !== this.loadVersion) return;

      const first = requests[0];
      const allMatch = requests.every((r) =>
        this.valueEquals(this.extractValue(r), this.extractValue(first)),
      );

      this.requests.set(requests);
      this.editCount.set(requests.length);
      this.tabState.set(allMatch ? 'ready' : 'mismatch');
      this.onLoaded(requests, allMatch ? this.extractValue(first) : null);
    } catch (err) {
      console.error(err);
      if (version === this.loadVersion) {
        this.tabState.set('unavailable');
      }
    }
  }

  protected async save(value: T): Promise<HttpRequest[]> {
    const requests = this.requests();
    if (requests.length === 0) return [];

    const version = this.loadVersion;
    this.saving.set(true);
    try {
      const saved = await this.saveFor(requests, value);
      this.applySaved(saved);
      if (version === this.loadVersion) {
        this.requests.set(saved);
        this.editCount.set(saved.length);
        this.tabState.set('ready');
      }
      return saved;
    } catch (err) {
      console.error(err);
      return [];
    } finally {
      this.saving.set(false);
    }
  }

  /**
   * Persists a value for the *current* requests without touching the tab's
   * local request list. Used by {@link beforeReload} so a pending save cannot
   * race the incoming selection.
   */
  protected async flush(value: T): Promise<void> {
    const requests = this.requests();
    if (requests.length === 0) return;
    try {
      this.applySaved(await this.saveFor(requests, value));
    } catch (err) {
      console.error(err);
    }
  }

  private isStale(active: HttpRequest): boolean {
    const current = this.requests().find((r) => r.id === active.id);
    if (!current) return true;
    return !this.valueEquals(this.extractValue(current), this.extractValue(active));
  }

  protected applySaved(saved: HttpRequest[]): void {
    const selectedId = this.state.selectedRequest()?.id;
    for (const updated of saved) {
      if (selectedId === updated.id) {
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
