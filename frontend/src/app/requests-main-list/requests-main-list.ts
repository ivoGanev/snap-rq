import { ChangeDetectionStrategy, Component, TemplateRef, ViewChild, computed, effect, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { CdkFixedSizeVirtualScroll, CdkVirtualForOf, CdkVirtualScrollViewport } from '@angular/cdk/scrolling';
import { WorkspaceStateService } from '../core/services/workspace-state.service';
import { RequestApiService, type HttpRequest, type HttpRequestSummary } from '../core/services/request.service';
import { ApiRequestsService } from '../core/services/api-requests.service';
import { FavouriteApiService, type FavouriteCollection } from '../core/services/favourite.service';
import { CollectionApiService, type Collection } from '../core/services/collection.service';
import { SelectionStateService } from '../core/services/selection-state.service';
import { TagApiService, type Tag } from '../core/services/tag.service';
import { BinApiService, type HttpBinnedRequestSummary, type RestoreBinnedRequestInput } from '../core/services/bin.service';
import { ContextMenuService } from '../core/services/context-menu.service';

type ColumnKey = 'name' | 'url' | 'method' | 'tags';
type SortDirection = 'asc' | 'desc';

interface SelectedCell {
  requestId: number;
  column: ColumnKey;
}

interface RowState {
  isSelected: boolean;
  isCellSelected: Record<ColumnKey, boolean>;
}

const COLUMNS: { key: ColumnKey; label: string }[] = [
  { key: 'name', label: 'Name' },
  { key: 'url', label: 'URL' },
  { key: 'method', label: 'Method' },
  { key: 'tags', label: 'Tags' },
];

const HTTP_METHODS = ['GET', 'POST', 'PUT', 'DELETE', 'PATCH', 'HEAD', 'OPTIONS'];
const DRAG_THRESHOLD_PX = 5;

/**
 * Spreadsheet-style request list with multi-select and bulk actions.
 */
@Component({
  selector: 'app-requests-main-list',
  imports: [FormsModule, CdkVirtualScrollViewport, CdkVirtualForOf, CdkFixedSizeVirtualScroll],
  templateUrl: './requests-main-list.html',
  styleUrl: './requests-main-list.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: {
    class: 'main-column',
    'aria-label': 'Requests',
    '(window:keydown)': 'onWindowKeydown($event)',
    '(window:mousemove)': 'onWindowMouseMove($event)',
    '(window:mouseup)': 'onWindowMouseUp()',
  },
})
export class RequestsMainList {
  protected readonly state = inject(WorkspaceStateService);
  private readonly requestApi = inject(RequestApiService);
  private readonly apiRequests = inject(ApiRequestsService);
  private readonly favouriteApi = inject(FavouriteApiService);
  private readonly collectionApi = inject(CollectionApiService);
  private readonly selectionState = inject(SelectionStateService);
  private readonly tagApi = inject(TagApiService);
  private readonly binApi = inject(BinApiService);
  private readonly contextMenu = inject(ContextMenuService);

  @ViewChild(CdkVirtualScrollViewport, { static: false })
  private readonly viewport!: CdkVirtualScrollViewport;

  protected readonly columns = COLUMNS;
  protected readonly httpMethods = HTTP_METHODS;
  protected readonly requestTags = this.tagApi.requestTags;
  protected readonly favouriteCollections = this.favouriteApi.collections;
  protected readonly favouriteMembership = this.favouriteApi.membership;
  protected readonly requestFavouriteIds = this.favouriteApi.requestsMembership;
  protected readonly collections = this.collectionApi.collections;

  readonly requestSearchQuery = signal('');
  readonly tagRequests = signal<HttpRequestSummary[]>([]);
  readonly newRequestPopupOpen = signal(false);
  readonly newRequestName = signal('My new snappy API');
  readonly newRequestUrl = signal('');
  readonly newRequestMethod = signal('GET');

  // Selection state
  readonly selectedRowIds = signal<Set<number>>(new Set());
  readonly selectedCell = signal<SelectedCell | null>(null);
  readonly lastClickedRowId = signal<number | null>(null);
  readonly isDragging = signal(false);
  readonly dragStartRowId = signal<number | null>(null);
  readonly dragStartClientY = signal(0);
  readonly dragHasMoved = signal(false);
  readonly ignoreNextClick = signal(false);

  readonly sortColumn = signal<ColumnKey | null>(null);
  readonly sortDirection = signal<SortDirection>('asc');

  // Single-cell edit modals
  readonly editModalOpen = signal(false);
  readonly editModalColumn = signal<ColumnKey | null>(null);
  readonly editModalRequest = signal<HttpRequestSummary | null>(null);
  readonly editModalValue = signal('');

  readonly methodModalOpen = signal(false);
  readonly methodModalRequest = signal<HttpRequestSummary | null>(null);
  readonly methodModalValue = signal('GET');

  readonly tagsModalOpen = signal(false);
  readonly tagsModalRequest = signal<HttpRequestSummary | null>(null);
  readonly tagsModalNewTagName = signal('');

  readonly favouritesModalOpen = signal(false);
  readonly favouritesModalRequest = signal<HttpRequestSummary | null>(null);
  readonly newFavouriteName = signal('');

  readonly newRequestPopupMode = signal<'manual' | 'curl'>('manual');
  readonly newRequestCurl = signal('');

  readonly moveCollectionModalOpen = signal(false);
  readonly bulkTagModalOpen = signal(false);
  readonly bulkTagName = signal('');
  readonly bulkFavouritesModalOpen = signal(false);

  // Restore collection picker (used when the original collection is gone).
  readonly restoreCollectionModalOpen = signal(false);
  readonly restorePickerBinIds = signal<number[]>([]);

  private loadVersion = 0;

  readonly activeRequests = computed<HttpRequestSummary[]>(() => {
    if (this.state.showingBin()) {
      return this.binApi.binnedRequests() as HttpRequestSummary[];
    }

    const tag = this.state.selectedTag();
    if (tag) {
      return this.tagRequests();
    }

    if (this.state.selectedFavouriteCollection()) {
      return this.favouriteApi.requests();
    }

    const collection = this.state.selectedCollection();
    if (collection) {
      return this.apiRequests.forCollection(collection.id);
    }

    if (this.state.showingAllRequests()) {
      const projectId = this.state.selectedProject()?.id;
      return projectId ? this.apiRequests.forProject(projectId) : [];
    }

    return [];
  });

  readonly activeGroupName = computed<string | null>(() => {
    if (this.state.showingBin()) return 'Trash bin';
    if (this.state.showingAllRequests()) return 'All requests';
    const tag = this.state.selectedTag();
    if (tag) return tag;
    const favourite = this.state.selectedFavouriteCollection();
    if (favourite) return favourite.name;
    const collection = this.state.selectedCollection();
    if (collection) return collection.name;
    return null;
  });

  readonly filteredActiveRequests = computed<HttpRequestSummary[]>(() => {
    const query = this.requestSearchQuery().trim().toLowerCase();
    const requests = this.activeRequests();
    let result = requests;

    if (query) {
      result = requests.filter(
        (req) =>
          req.name.toLowerCase().includes(query) ||
          req.url.toLowerCase().includes(query) ||
          req.method.toLowerCase().includes(query),
      );
    }

    const sortCol = this.sortColumn();
    if (sortCol) {
      result = [...result].sort((a, b) => this.compareRequests(a, b, sortCol));
    }

    return result;
  });

  /**
   * Precomputed per-row view state, keyed by request id, for template bindings.
   *
   * Why: the template used to call `isRowSelected(req)`,
   * `isCellSelected(req, col)` and `getFavouriteCount(req)` directly — that is
   * ~9 method calls per rendered row on every change-detection pass (a
   * 1,000-row list re-ran ~9,000 calls each time any signal changed).
   *
   * Instead this computed builds ONE lookup table in a single pass, and only
   * re-runs when a dependency signal actually changed (row selection, the
   * selected cell, favourite membership, or the filtered/sorted list itself).
   * The template then performs cheap dictionary reads like
   * `rowState()[req.id]?.isSelected`.
   */
  readonly rowState = computed<Record<number, RowState>>(() => {
    const selectedIds = this.selectedRowIds();
    const cell = this.selectedCell();
    const result: Record<number, RowState> = {};

    for (const req of this.filteredActiveRequests()) {
      const isSelected = selectedIds.has(req.id);
      // The dashed cell outline is only shown for single-row selection; when
      // multiple rows are selected the outline would be meaningless noise.
      const isCellSelected: Record<ColumnKey, boolean> = {
        name: false,
        url: false,
        method: false,
        tags: false,
      };
      if (isSelected && selectedIds.size <= 1 && cell?.requestId === req.id) {
        isCellSelected[cell.column] = true;
      }
      result[req.id] = {
        isSelected,
        isCellSelected,
      };
    }
    return result;
  });

  readonly selectedRequest = computed<HttpRequestSummary | null>(() => {
    const cell = this.selectedCell();
    if (!cell) return null;
    return this.activeRequests().find((r) => r.id === cell.requestId) ?? null;
  });

  readonly selectedRequests = computed<HttpRequestSummary[]>(() => {
    const ids = this.selectedRowIds();
    return this.activeRequests().filter((r) => ids.has(r.id));
  });

  readonly tagSuggestions = computed<Tag[]>(() => {
    const req = this.tagsModalRequest();
    if (!req) return [];

    const query = this.tagsModalNewTagName().trim().toLowerCase();
    const tags = this.tagApi.allTags();
    const existing = new Set(this.requestTags()[req.id] ?? []);

    if (!query) {
      return tags.filter((tag) => !existing.has(tag.name)).slice(0, 6);
    }
    return tags
      .filter((tag) => tag.name.toLowerCase().includes(query) && !existing.has(tag.name))
      .slice(0, 6);
  });

  readonly bulkTagSuggestions = computed<Tag[]>(() => {
    const query = this.bulkTagName().trim().toLowerCase();
    const tags = this.tagApi.allTags();
    if (!query) return tags.slice(0, 6);
    return tags.filter((tag) => tag.name.toLowerCase().includes(query)).slice(0, 6);
  });

  readonly availableCollectionsForMove = computed<Collection[]>(() => {
    const current = this.state.selectedCollection();
    return this.collectionApi.collections().filter((c) => c.id !== current?.id);
  });

  constructor() {
    effect(() => {
      const collection = this.state.selectedCollection();
      const favourite = this.state.selectedFavouriteCollection();
      const tag = this.state.selectedTag();
      const showAll = this.state.showingAllRequests();
      const showBin = this.state.showingBin();
      const project = this.state.selectedProject();

      const version = ++this.loadVersion;
      this.requestSearchQuery.set('');
      this.clearSelection();
      this.state.selectedRequest.set(null);
      this.state.selectedResponse.set(null);
      this.state.selectedBinnedRequest.set(null);
      this.state.multiSelectionActive.set(false);

      void this.loadActiveGroup(version, collection?.id ?? null, favourite?.id ?? null, tag, showAll, showBin, project?.id ?? null);
    });

    effect(() => {
      this.state.multiSelectionActive.set(this.selectedRowIds().size > 1);
    });
  }

  private async loadActiveGroup(
    version: number,
    collectionId: number | null,
    favouriteId: number | null,
    tag: string | null,
    showAll: boolean,
    showBin: boolean,
    projectId: number | null,
  ): Promise<void> {
    try {
      if (showBin && projectId !== null) {
        await this.binApi.loadForProject(projectId);
        if (version !== this.loadVersion) return;
        return;
      }

      if (tag) {
        const project = this.state.selectedProject();
        if (!project) return;
        const requests = await this.tagApi.getRequestsForTag(project.id, tag);
        if (version !== this.loadVersion) return;
        this.tagRequests.set(requests);
        await this.tagApi.loadTagsForRequests(requests);
        this.favouriteApi.requestsMembership.set({});
        return;
      }

      if (favouriteId !== null) {
        await this.favouriteApi.loadRequestsForCollection(favouriteId);
        if (version !== this.loadVersion) return;
        const requests = this.favouriteApi.requests();
        await this.tagApi.loadTagsForRequests(requests);
        const rememberedId = this.selectionState.getSelectedRequestForFavourite(favouriteId);
        this.restoreRememberedRequest(requests, rememberedId);
        return;
      }

      if (collectionId !== null) {
        if (version !== this.loadVersion) return;
        const requests = this.apiRequests.forCollection(collectionId);
        await this.tagApi.loadTagsForRequests(requests);
        const rememberedId = this.selectionState.getSelectedRequestForCollection(collectionId);
        this.restoreRememberedRequest(requests, rememberedId);
        return;
      }

      if (showAll && projectId !== null) {
        if (version !== this.loadVersion) return;
        const requests = this.apiRequests.forProject(projectId);
        await this.tagApi.loadTagsForRequests(requests);
        return;
      }

      this.tagRequests.set([]);
      this.favouriteApi.requestsMembership.set({});
    } catch (err) {
      console.error(err);
    }
  }

  private restoreRememberedRequest(requests: HttpRequestSummary[], rememberedId: number | null): void {
    if (rememberedId === null) return;
    const index = requests.findIndex((r) => r.id === rememberedId);
    if (index === -1) return;

    const remembered = requests[index];
    this.selectCell(remembered, 'name');

    // Ensure the remembered row is visible in the virtual viewport.
    if (this.viewport) {
      this.viewport.scrollToIndex(index, 'auto');
    }
  }

  onWindowKeydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      this.onEscapePressed();
      return;
    }

    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'd') {
      event.preventDefault();
      if (this.state.showingBin()) return;
      void this.duplicateSelectedRequest();
    }
  }

  onEscapePressed(): void {
    if (this.contextMenu.isOpen()) {
      this.contextMenu.close();
      return;
    }
    if (this.editModalOpen()) {
      this.closeEditModal();
      return;
    }
    if (this.methodModalOpen()) {
      this.closeMethodModal();
      return;
    }
    if (this.tagsModalOpen()) {
      this.closeTagsModal();
      return;
    }
    if (this.favouritesModalOpen()) {
      this.closeFavouritesModal();
      return;
    }
    if (this.moveCollectionModalOpen()) {
      this.closeMoveCollectionModal();
      return;
    }
    if (this.bulkTagModalOpen()) {
      this.closeBulkTagModal();
      return;
    }
    if (this.bulkFavouritesModalOpen()) {
      this.closeBulkFavouritesModal();
      return;
    }
    if (this.newRequestPopupOpen()) {
      this.closeNewRequestPopup();
      return;
    }
  }

  // ---------- Selection ----------

  selectCell(req: HttpRequestSummary | HttpBinnedRequestSummary, column: ColumnKey): void {
    this.selectedRowIds.set(new Set([req.id]));
    this.selectedCell.set({ requestId: req.id, column });
    this.syncWorkspaceSelection(req);

    if (this.state.showingBin()) {
      return;
    }

    const collection = this.state.selectedCollection();
    if (collection) {
      this.selectionState.setSelectedRequestForCollection(collection.id, req.id);
      return;
    }

    const favourite = this.state.selectedFavouriteCollection();
    if (favourite) {
      this.selectionState.setSelectedRequestForFavourite(favourite.id, req.id);
    }
  }

  private clearSelection(): void {
    this.selectedRowIds.set(new Set());
    this.selectedCell.set(null);
  }

  trackById(_index: number, req: HttpRequestSummary): number {
    return req.id;
  }

  onRowClick(req: HttpRequestSummary | HttpBinnedRequestSummary, event: MouseEvent, column: ColumnKey): void {
    if (event.button !== 0) return;

    if (this.ignoreNextClick()) {
      this.ignoreNextClick.set(false);
      return;
    }

    if (event.shiftKey) {
      this.selectRangeTo(req, column);
      return;
    }

    if (event.ctrlKey) {
      this.toggleRowSelection(req, column);
      return;
    }

    // Plain click: clear existing multi-selection and select this row only.
    this.selectCell(req, column);
    this.lastClickedRowId.set(req.id);
  }

  private toggleRowSelection(req: HttpRequestSummary | HttpBinnedRequestSummary, column: ColumnKey): void {
    const set = new Set(this.selectedRowIds());
    if (set.has(req.id)) {
      set.delete(req.id);
    } else {
      set.add(req.id);
    }
    this.selectedRowIds.set(set);
    this.selectedCell.set({ requestId: req.id, column });
    this.lastClickedRowId.set(req.id);
    this.syncWorkspaceSelection(req);
  }

  private syncWorkspaceSelection(fallbackReq: HttpRequestSummary | HttpBinnedRequestSummary | null): void {
    const set = this.selectedRowIds();
    if (set.size > 1) {
      this.state.selectedRequest.set(null);
      this.state.selectedBinnedRequest.set(null);
      this.state.selectedResponse.set(null);
      return;
    }

    if (set.size === 0) {
      this.state.selectedRequest.set(null);
      this.state.selectedBinnedRequest.set(null);
      this.state.selectedResponse.set(null);
      return;
    }

    const id = [...set][0];

    if (this.state.showingBin()) {
      const binned = this.activeRequests().find(r => r.id === id) ?? null;
      this.state.selectedBinnedRequest.set(binned as HttpBinnedRequestSummary | null);
      this.state.selectedRequest.set(null);
      this.state.selectedResponse.set(null);
      return;
    }

    const current = this.state.selectedRequest();
    if (current?.id === id) {
      return;
    }

    if (this.isDragging()) {
      // During drag selection only clear the workspace detail; load the full
      // request once the drag ends to avoid one IPC call per row crossed.
      this.state.selectedRequest.set(null);
      this.state.selectedResponse.set(null);
      return;
    }

    this.state.selectedRequest.set(null);
    this.state.selectedResponse.set(null);

    void this.requestApi.get(id).then(full => {
      if (this.selectedRowIds().has(id)) {
        this.state.selectedRequest.set(full);
      }
    }).catch(err => console.error(err));
  }

  private selectRangeTo(req: HttpRequestSummary | HttpBinnedRequestSummary, column: ColumnKey): void {
    const anchor = this.lastClickedRowId();
    const visible = this.filteredActiveRequests();
    const ids = visible.map((r) => r.id);
    const anchorIndex = anchor !== null ? ids.indexOf(anchor) : -1;
    const targetIndex = ids.indexOf(req.id);

    if (anchorIndex === -1 || targetIndex === -1) {
      this.selectCell(req, column);
      return;
    }

    const start = Math.min(anchorIndex, targetIndex);
    const end = Math.max(anchorIndex, targetIndex);
    const next = new Set(this.selectedRowIds());
    for (let i = start; i <= end; i++) {
      next.add(visible[i].id);
    }
    this.selectedRowIds.set(next);
    this.selectedCell.set({ requestId: req.id, column });
    this.syncWorkspaceSelection(req);
  }

  // ---------- Drag selection ----------

  onRowMouseDown(req: HttpRequestSummary | HttpBinnedRequestSummary, event: MouseEvent): void {
    if (event.button !== 0) return;
    // A new mousedown always starts a new gesture: clear any stale
    // ignoreNextClick left over from a previous drag release whose trailing
    // click never landed on a row. Without this, the first deliberate click
    // after a drag would be swallowed (requiring two clicks to select).
    this.ignoreNextClick.set(false);
    if (event.ctrlKey || event.shiftKey) return;

    this.dragStartRowId.set(req.id);
    this.dragStartClientY.set(event.clientY);
    this.dragHasMoved.set(false);
    this.isDragging.set(false);
  }

  onWindowMouseMove(event: MouseEvent): void {
    if (this.dragStartRowId() === null) return;

    if (!this.isDragging()) {
      const delta = Math.abs(event.clientY - this.dragStartClientY());
      if (delta > DRAG_THRESHOLD_PX) {
        this.isDragging.set(true);
        this.dragHasMoved.set(true);
        const startReq = this.activeRequests().find((r) => r.id === this.dragStartRowId());
        if (startReq) {
          this.selectedRowIds.set(new Set([startReq.id]));
          this.selectedCell.set({ requestId: startReq.id, column: 'name' });
          this.lastClickedRowId.set(startReq.id);
          this.syncWorkspaceSelection(startReq);
        }
      }
    }
  }

  /**
   * Extends an in-progress drag selection to the row under the cursor.
   *
   * Why not per-row `mouseenter` (the old approach): with virtual scrolling
   * only the ~15-20 visible rows exist in the DOM. Rows scrolled out of view
   * have no element and would never fire events, and fast drags would skip
   * rendered rows entirely. So we locate the target row arithmetically:
   *
   *   relativeY     = mouse Y inside the visible viewport (px from its top edge)
   *   scrollOffset  = how far the list is currently scrolled down (px)
   *   relativeY + scrollOffset = mouse Y within the *entire* virtual content
   *
   * Dividing that by the fixed row height (40px, must match the viewport's
   * `itemSize`) yields the index of the row under the cursor, which is then
   * clamped to the list bounds. Finally we select every row between the drag
   * start row and the row under the cursor.
   */
  onViewportMouseMove(event: MouseEvent): void {
    if (!this.isDragging() || this.dragStartRowId() === null) return;

    const viewport = this.viewport.elementRef.nativeElement;
    const rect = viewport.getBoundingClientRect();
    const relativeY = event.clientY - rect.top;
    const scrollOffset = this.viewport.measureScrollOffset('top');
    const rowHeight = 40;
    // Clamp so dragging above the first row or below the last row (or past the
    // bottom edge of the content) keeps the selection within the list.
    const index = Math.max(
      0,
      Math.min(
        Math.floor((scrollOffset + relativeY) / rowHeight),
        this.filteredActiveRequests().length - 1,
      ),
    );

    const requests = this.filteredActiveRequests();
    const currentReq = requests[index];
    if (!currentReq) return;

    const startIndex = requests.findIndex(r => r.id === this.dragStartRowId());
    if (startIndex === -1) return;

    // Select the whole span between the drag anchor and the current row.
    const rangeStart = Math.min(startIndex, index);
    const rangeEnd = Math.max(startIndex, index);
    const next = new Set<number>();
    for (let i = rangeStart; i <= rangeEnd; i++) {
      next.add(requests[i].id);
    }

    this.selectedRowIds.set(next);
    this.selectedCell.set({ requestId: currentReq.id, column: 'name' });
  }

  onWindowMouseUp(): void {
    if (this.dragStartRowId() === null) return;

    const wasDragging = this.isDragging() || this.dragHasMoved();
    if (wasDragging) {
      this.ignoreNextClick.set(true);
    }

    this.dragStartRowId.set(null);
    this.dragStartClientY.set(0);
    this.isDragging.set(false);
    this.dragHasMoved.set(false);

    if (wasDragging) {
      const req = this.activeRequests().find(r => this.selectedRowIds().has(r.id)) ?? null;
      if (req) {
        this.syncWorkspaceSelection(req);
      }
    }
  }

  // ---------- Context menu ----------

  onRowContextMenu(
    req: HttpRequestSummary | HttpBinnedRequestSummary,
    event: MouseEvent,
    template: TemplateRef<{ $implicit: HttpRequestSummary | HttpBinnedRequestSummary }>,
  ): void {
    event.preventDefault();
    event.stopPropagation();

    if (!this.selectedRowIds().has(req.id)) {
      this.selectCell(req, 'name');
      this.lastClickedRowId.set(req.id);
    }

    this.contextMenu.open(template, req, event.clientX, event.clientY);
  }

  async copyRequestCurl(req?: HttpRequestSummary | HttpBinnedRequestSummary): Promise<void> {
    if (!req) return;

    try {
      const full = await this.requestApi.get(req.id);
      const curl = await this.requestApi.requestToCurl(full);
      await navigator.clipboard.writeText(curl);
    } catch (err) {
      console.error(err);
    } finally {
      this.contextMenu.close();
    }
  }

  async duplicateSelectedRequest(): Promise<void> {
    const requests = this.selectedRequests();
    if (requests.length === 0) return;

    this.contextMenu.close();
    this.state.loading.set(true);

    try {
      const duplicated: { originalId: number; request: HttpRequestSummary }[] = [];
      for (const original of requests) {
        duplicated.push({ originalId: original.id, request: await this.apiRequests.duplicate(original.id) });
      }

      const favourite = this.state.selectedFavouriteCollection();
      if (favourite) {
        // Duplicates inherit favourite memberships, so keep the local list in sync.
        const inThisFavourite = duplicated
          .filter(d => (this.requestFavouriteIds()[d.originalId] ?? []).includes(favourite.id))
          .map(d => d.request);
        if (inThisFavourite.length > 0) {
          this.favouriteApi.requests.update(list =>
            [...list, ...inThisFavourite].sort((a, b) => a.name.localeCompare(b.name)),
          );
        }
      }

      const duplicatedRequests = duplicated.map(d => d.request);
      await Promise.all([
        this.tagApi.loadTagsForRequests(duplicatedRequests),
        this.favouriteApi.loadMembershipForRequests(duplicatedRequests),
      ]);

      const lastDuplicated = duplicatedRequests[duplicatedRequests.length - 1] ?? null;
      if (lastDuplicated) {
        this.selectCell(lastDuplicated, 'name');
      }
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }

  async deleteSelectedRequests(): Promise<void> {
    const ids = [...this.selectedRowIds()];
    if (ids.length === 0) return;

    this.contextMenu.close();
    this.state.loading.set(true);

    try {
      if (this.state.showingBin()) {
        await this.binApi.deleteManyPermanently(ids);
        this.state.selectedBinnedRequest.set(null);
        this.clearSelection();
        return;
      }

      const favourite = this.state.selectedFavouriteCollection();
      const collection = this.state.selectedCollection();
      const tag = this.state.selectedTag();

      if (favourite) {
        for (const id of ids) {
          await this.favouriteApi.removeRequest(favourite.id, id);
        }
        this.favouriteApi.requests.update(list => list.filter(r => !ids.includes(r.id)));
      } else {
        await this.apiRequests.deleteMany(ids);
        for (const id of ids) {
          this.selectionState.deleteRequest(id);
        }
      }

      if (this.state.selectedRequest() && ids.includes(this.state.selectedRequest()!.id)) {
        this.state.selectedRequest.set(null);
        this.state.selectedResponse.set(null);
      }

      this.clearSelection();

      if (tag) {
        this.tagRequests.update(list => list.filter(r => !ids.includes(r.id)));
        await this.tagApi.loadTagsForRequests(this.tagRequests());
      }
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }

  // ---------- Restore from bin ----------

  openRestoreCollectionPicker(): void {
    this.contextMenu.close();
    const selectedIds = [...this.selectedRowIds()];
    const binned = this.binApi.binnedRequests().filter(r => selectedIds.includes(r.id));
    const collections = this.collectionApi.collections();
    const missingOriginal = binned.some(r => !collections.some(c => c.id === r.original_collection_id));

    if (!missingOriginal) {
      const inputs = binned.map(r => ({
        bin_id: r.id,
        target_collection_id: r.original_collection_id,
      }));
      void this.doRestore(inputs);
      return;
    }

    this.restorePickerBinIds.set(selectedIds);
    this.restoreCollectionModalOpen.set(true);
  }

  closeRestoreCollectionModal(): void {
    this.restoreCollectionModalOpen.set(false);
    this.restorePickerBinIds.set([]);
  }

  async restoreSelectedRequestsToCollection(collection: Collection): Promise<void> {
    const selectedIds = this.restorePickerBinIds();
    const binned = this.binApi.binnedRequests().filter(r => selectedIds.includes(r.id));
    const collections = this.collectionApi.collections();

    const inputs = binned.map(r => {
      const originalExists = collections.some(c => c.id === r.original_collection_id);
      return {
        bin_id: r.id,
        target_collection_id: originalExists ? r.original_collection_id : collection.id,
      };
    });

    await this.doRestore(inputs);
    this.closeRestoreCollectionModal();
  }

  private async doRestore(inputs: RestoreBinnedRequestInput[]): Promise<void> {
    if (inputs.length === 0) return;

    this.state.loading.set(true);
    try {
      await this.binApi.restoreMany(inputs);
      this.state.selectedBinnedRequest.set(null);
      this.clearSelection();
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }

  // ---------- Move to Collection ----------

  openMoveCollectionModal(): void {
    this.contextMenu.close();
    this.moveCollectionModalOpen.set(true);
  }

  closeMoveCollectionModal(): void {
    this.moveCollectionModalOpen.set(false);
  }

  async moveSelectedRequestsToCollection(collection: Collection): Promise<void> {
    if (this.state.showingBin()) return;
    const requests = this.selectedRequests() as HttpRequestSummary[];
    if (requests.length === 0) return;

    this.state.loading.set(true);
    try {
      for (const req of requests) {
        await this.apiRequests.move(req.id, collection.id, collection.project_id);
      }

      this.clearSelection();
      this.closeMoveCollectionModal();
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }

  // ---------- Bulk Tag ----------

  openBulkTagModal(): void {
    this.contextMenu.close();
    this.bulkTagName.set('');
    this.bulkTagModalOpen.set(true);
  }

  closeBulkTagModal(): void {
    this.bulkTagModalOpen.set(false);
    this.bulkTagName.set('');
  }

  async addBulkTag(tagName: string): Promise<void> {
    if (this.state.showingBin()) return;
    const name = tagName.trim();
    const project = this.state.selectedProject();
    if (!name || !project) return;

    const requests = this.selectedRequests() as HttpRequestSummary[];
    if (requests.length === 0) return;

    this.state.loading.set(true);
    try {
      for (const req of requests) {
        await this.tagApi.addTagToRequest(req.id, project.id, name);
      }
      this.bulkTagName.set('');
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }

  // ---------- Bulk Favourites ----------

  openBulkFavouritesModal(): void {
    this.contextMenu.close();
    const requests = this.selectedRequests();
    if (requests.length > 0) {
      void this.favouriteApi.loadMembershipForRequests(requests);
    }
    this.bulkFavouritesModalOpen.set(true);
  }

  closeBulkFavouritesModal(): void {
    this.bulkFavouritesModalOpen.set(false);
    this.favouriteApi.clearMembership();
  }

  isAllSelectedInCollection(collectionId: number): boolean {
    if (this.state.showingBin()) return false;
    const requests = this.selectedRequests() as HttpRequestSummary[];
    if (requests.length === 0) return false;
    return requests.every((req) => (this.requestFavouriteIds()[req.id] ?? []).includes(collectionId));
  }

  async toggleBulkFavouriteMembership(collection: FavouriteCollection): Promise<void> {
    if (this.state.showingBin()) return;
    const requests = this.selectedRequests() as HttpRequestSummary[];
    if (requests.length === 0) return;

    const allIn = this.isAllSelectedInCollection(collection.id);
    this.state.loading.set(true);
    try {
      for (const req of requests) {
        if (allIn) {
          await this.favouriteApi.removeRequest(collection.id, req.id);
        } else {
          await this.favouriteApi.addRequest(collection.id, req.id);
        }
      }

      const ids = requests.map(r => r.id);
      this.favouriteApi.requestsMembership.update(map => {
        const next = { ...map };
        for (const id of ids) {
          const current = next[id] ?? [];
          next[id] = allIn
            ? current.filter(cid => cid !== collection.id)
            : Array.from(new Set([...current, collection.id]));
        }
        return next;
      });

      const currentFavourite = this.state.selectedFavouriteCollection();
      if (currentFavourite?.id === collection.id) {
        if (allIn) {
          this.favouriteApi.requests.update(list => list.filter(r => !ids.includes(r.id)));
        } else {
          this.favouriteApi.requests.update(list => {
            const existingIds = new Set(list.map(r => r.id));
            const added = requests.filter(r => !existingIds.has(r.id));
            return added.length > 0
              ? [...list, ...added].sort((a, b) => a.name.localeCompare(b.name))
              : list;
          });
        }
      }
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }

  // ---------- Sorting ----------

  toggleSort(column: ColumnKey): void {
    if (this.sortColumn() === column) {
      this.sortDirection.update((d) => (d === 'asc' ? 'desc' : 'asc'));
    } else {
      this.sortColumn.set(column);
      this.sortDirection.set('asc');
    }
  }

  sortIndicator(column: ColumnKey): '' | 'asc' | 'desc' {
    if (this.sortColumn() !== column) return '';
    return this.sortDirection();
  }

  private compareRequests(a: HttpRequestSummary, b: HttpRequestSummary, column: ColumnKey): number {
    const dir = this.sortDirection() === 'asc' ? 1 : -1;

    switch (column) {
      case 'name':
        return a.name.localeCompare(b.name) * dir;
      case 'url':
        return a.url.localeCompare(b.url) * dir;
      case 'method':
        return a.method.localeCompare(b.method) * dir;
      case 'tags': {
        const aTags = (this.requestTags()[a.id] ?? []).join(', ');
        const bTags = (this.requestTags()[b.id] ?? []).join(', ');
        return aTags.localeCompare(bTags) * dir;
      }
      default:
        return 0;
    }
  }

  getCellValue(req: HttpRequestSummary, column: ColumnKey): string {
    switch (column) {
      case 'name':
        return req.name;
      case 'url':
        return req.url;
      case 'method':
        return req.method;
      case 'tags':
        return (this.requestTags()[req.id] ?? []).join(', ');
      default:
        return '';
    }
  }

  // ---------- New request ----------

  openNewRequestPopup(): void {
    this.contextMenu.close();
    this.newRequestName.set('My new snappy API');
    this.newRequestUrl.set('');
    this.newRequestMethod.set('GET');
    this.newRequestCurl.set('');
    this.newRequestPopupMode.set('manual');
    this.newRequestPopupOpen.set(true);
  }

  closeNewRequestPopup(): void {
    this.newRequestPopupOpen.set(false);
  }

  setNewRequestPopupMode(mode: 'manual' | 'curl'): void {
    this.newRequestPopupMode.set(mode);
  }

  async addRequest(): Promise<void> {
    const collection = this.state.selectedCollection();
    if (!collection) return;

    const mode = this.newRequestPopupMode();
    if (mode === 'curl') {
      await this.addRequestFromCurl(collection.id);
      return;
    }

    const name = this.newRequestName().trim();
    if (!name) return;

    this.state.loading.set(true);
    try {
      await this.apiRequests.create({
        collection_id: collection.id,
        project_id: collection.project_id,
        name,
        url: this.newRequestUrl().trim(),
        method: this.newRequestMethod(),
        body: '',
        request_headers: '',
        status_code: 0,
        response_id: 0,
      });
      this.closeNewRequestPopup();
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }

  private async addRequestFromCurl(collectionId: number): Promise<void> {
    const curl = this.newRequestCurl().trim();
    if (!curl) return;

    this.state.loading.set(true);
    try {
      const projectId = this.state.selectedProject()?.id ?? 0;
      const req = await this.requestApi.curlToRequest(collectionId, curl);
      await this.apiRequests.create({
        collection_id: collectionId,
        project_id: projectId,
        name: req.name,
        url: req.url,
        method: req.method,
        body: req.body,
        request_headers: req.request_headers,
        status_code: 0,
        response_id: 0,
      });
      this.closeNewRequestPopup();
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }

  async sendSelectedRequest(event: MouseEvent): Promise<void> {
    if (this.state.showingBin()) return;

    let req = this.state.selectedRequest();
    if (!req) {
      const selectedId = this.selectedCell()?.requestId ?? [...this.selectedRowIds()][0];
      if (!selectedId) return;
      try {
        req = await this.requestApi.get(selectedId);
      } catch (err) {
        console.error(err);
        return;
      }
    }
    await this.state.sendRequest(req, event);
  }

  onFavouriteButtonClick(): void {
    if (this.selectedRowIds().size <= 1) {
      const req = this.selectedRequest() ?? this.selectedRequests()[0];
      if (req) {
        this.openFavouritesModal(req);
      }
    } else {
      this.openBulkFavouritesModal();
    }
  }

  openZenMode(req: HttpRequestSummary | HttpBinnedRequestSummary, event: MouseEvent): void {
    event.stopPropagation();
    this.contextMenu.close();
    if (this.state.showingBin()) return;
    this.selectCell(req as HttpRequestSummary, 'name');
    this.state.zenModeOpen.set(true);
  }

  onCellDoubleClick(req: HttpRequestSummary | HttpBinnedRequestSummary, column: ColumnKey): void {
    if (this.state.showingBin()) return;
    const request = req as HttpRequestSummary;
    if (column === 'method') {
      this.openMethodModal(request);
      return;
    }
    if (column === 'tags') {
      this.openTagsModal(request);
      return;
    }
    this.openEditModal(request, column);
  }

  // ---------- Text edit modal ----------

  openEditModal(req: HttpRequestSummary, column: ColumnKey): void {
    this.contextMenu.close();
    this.editModalRequest.set(req);
    this.editModalColumn.set(column);
    this.editModalValue.set(this.getCellValue(req, column));
    this.editModalOpen.set(true);
  }

  closeEditModal(): void {
    this.editModalOpen.set(false);
    this.editModalRequest.set(null);
    this.editModalColumn.set(null);
    this.editModalValue.set('');
  }

  async saveEditModal(): Promise<void> {
    const req = this.editModalRequest();
    const column = this.editModalColumn();
    if (!req || !column) return;

    if (column === 'tags') return;

    const field = this.columnToRequestField(column);
    await this.updateRequestField(req, field, this.editModalValue());
    this.closeEditModal();
  }

  // ---------- Method modal ----------

  openMethodModal(req: HttpRequestSummary): void {
    this.contextMenu.close();
    this.methodModalRequest.set(req);
    this.methodModalValue.set(req.method);
    this.methodModalOpen.set(true);
  }

  closeMethodModal(): void {
    this.methodModalOpen.set(false);
    this.methodModalRequest.set(null);
    this.methodModalValue.set('GET');
  }

  async saveMethodModal(): Promise<void> {
    const req = this.methodModalRequest();
    if (!req) return;

    await this.updateRequestField(req, 'method', this.methodModalValue());
    this.closeMethodModal();
  }

  // ---------- Tags modal (single request) ----------

  openTagsModal(req: HttpRequestSummary): void {
    this.contextMenu.close();
    this.tagsModalRequest.set(req);
    this.tagsModalNewTagName.set('');
    this.tagsModalOpen.set(true);
  }

  closeTagsModal(): void {
    this.tagsModalOpen.set(false);
    this.tagsModalRequest.set(null);
    this.tagsModalNewTagName.set('');
  }

  async addTagToRequest(req: HttpRequestSummary, tagName: string): Promise<void> {
    const name = tagName.trim();
    const project = this.state.selectedProject();
    if (!name || !project) return;

    try {
      await this.tagApi.addTagToRequest(req.id, project.id, name);
      this.tagsModalNewTagName.set('');
    } catch (err) {
      console.error(err);
    }
  }

  async removeTagFromRequest(req: HttpRequestSummary, tagName: string, event: MouseEvent): Promise<void> {
    event.stopPropagation();
    try {
      await this.tagApi.removeTagFromRequest(req.id, tagName);
    } catch (err) {
      console.error(err);
    }
  }

  // ---------- Favourites modal (single request) ----------

  openFavouritesModal(req: HttpRequestSummary): void {
    this.contextMenu.close();
    this.favouritesModalRequest.set(req);
    this.newFavouriteName.set('');
    this.favouritesModalOpen.set(true);
    void this.favouriteApi.loadMembershipForRequest(req.id);
  }

  closeFavouritesModal(): void {
    this.favouritesModalOpen.set(false);
    this.favouritesModalRequest.set(null);
    this.favouriteApi.clearMembership();
  }

  async toggleFavouriteMembership(collection: FavouriteCollection): Promise<void> {
    const req = this.favouritesModalRequest();
    if (!req) return;

    const isMember = this.favouriteMembership().has(collection.id);
    try {
      if (isMember) {
        await this.favouriteApi.removeRequest(collection.id, req.id);
      } else {
        await this.favouriteApi.addRequest(collection.id, req.id);
      }

      this.favouriteApi.requestsMembership.update(map => {
        const current = map[req.id] ?? [];
        return {
          ...map,
          [req.id]: isMember
            ? current.filter(cid => cid !== collection.id)
            : Array.from(new Set([...current, collection.id])),
        };
      });

      if (this.state.selectedFavouriteCollection()?.id === collection.id) {
        this.favouriteApi.requests.update(list =>
          isMember ? list.filter(r => r.id !== req.id) : [...list, req].sort((a, b) => a.name.localeCompare(b.name)),
        );
      }
    } catch (err) {
      console.error(err);
    }
  }

  async addFavouriteCollection(): Promise<void> {
    const name = this.newFavouriteName().trim();
    const project = this.state.selectedProject();
    if (!name || !project) return;

    try {
      await this.favouriteApi.createCollection({ project_id: project.id, name });
      this.newFavouriteName.set('');
    } catch (err) {
      console.error(err);
    }
  }

  async deleteFavouriteCollection(
    collection: FavouriteCollection,
    event: MouseEvent,
  ): Promise<void> {
    event.stopPropagation();
    try {
      await this.favouriteApi.deleteCollection(collection.id);
      if (this.state.selectedFavouriteCollection()?.id === collection.id) {
        this.state.selectedFavouriteCollection.set(null);
        this.favouriteApi.requests.set([]);
      }
    } catch (err) {
      console.error(err);
    }
  }

  // ---------- Request updates ----------

  private columnToRequestField(column: ColumnKey): keyof HttpRequestSummary {
    switch (column) {
      case 'url':
        return 'url';
      case 'method':
        return 'method';
      case 'name':
      default:
        return 'name';
    }
  }

  private async updateRequestField<K extends keyof HttpRequestSummary>(
    req: HttpRequestSummary,
    field: K,
    value: HttpRequestSummary[K],
  ): Promise<void> {
    this.state.loading.set(true);
    try {
      const full = await this.requestApi.get(req.id);
      const updated = { ...full, [field]: value } as HttpRequest;
      const saved = await this.apiRequests.update(updated);
      this.patchRequestInLists(saved.id, saved);

      const selected = this.state.selectedRequest();
      if (selected?.id === saved.id) {
        this.state.selectedRequest.set(saved);
      }
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }

  private patchRequestInLists(id: number, changes: Partial<HttpRequestSummary>): void {
    this.apiRequests.patch(id, changes);

    const patch = (list: HttpRequestSummary[]) => {
      const index = list.findIndex((r) => r.id === id);
      if (index === -1) return list;
      const next = [...list];
      next[index] = { ...next[index], ...changes };
      return next;
    };

    this.favouriteApi.requests.update(patch);
    this.tagRequests.update(patch);
  }
}
