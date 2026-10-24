import { ChangeDetectionStrategy, Component, ElementRef, OnDestroy, TemplateRef, ViewChild, computed, effect, inject, signal, untracked, viewChild } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { CdkFixedSizeVirtualScroll, CdkVirtualForOf, CdkVirtualScrollViewport } from '@angular/cdk/scrolling';
import { Clipboard } from '@wailsio/runtime';
import { WorkspaceStateService } from '../core/services/workspace-state.service';
import { RequestApiService, type HttpRequest, type HttpRequestSummary } from '../core/services/request.service';
import { ApiRequestsService } from '../core/services/api-requests.service';
import { FavouriteApiService, type FavouriteCollection } from '../core/services/favourite.service';
import { CollectionApiService, type Collection } from '../core/services/collection.service';
import { SelectionStateService, groupKey } from '../core/services/selection-state.service';
import { TagApiService, type Tag } from '../core/services/tag.service';
import { BinApiService, type HttpBinnedRequestSummary, type RestoreBinnedRequestInput } from '../core/services/bin.service';
import { ContextMenuService } from '../core/services/context-menu.service';
import { RequestBottomDrawer, type BottomTab } from '../request-bottom-drawer/request-bottom-drawer';
import { RequestBodyTab } from '../request-body-tab/request-body-tab';
import { RequestHeadersTab } from '../request-headers-tab/request-headers-tab';
import { USER_ORDERABLE_VIEWS, type RequestListViewKind } from './request-view-ordering';

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

interface ReorderDrag {
  /** Snapshot of the dragged row shown in the ghost. */
  request: HttpRequestSummary;
  fromIndex: number;
}

const BOTTOM_TABS: BottomTab[] = [
  { id: 'body', label: 'Body', icon: 'body', component: RequestBodyTab },
  { id: 'headers', label: 'Headers', icon: 'headers', component: RequestHeadersTab },
];

const HTTP_METHODS = ['GET', 'POST', 'PUT', 'DELETE', 'PATCH', 'HEAD', 'OPTIONS'];
const DRAG_THRESHOLD_PX = 5;
const REORDER_EDGE_SCROLL_PX = 44;
const REORDER_EDGE_SCROLL_STEP_PX = 10;
const DROP_HIGHLIGHT_MS = 500;
const METHOD_COLUMN_MIN_WIDTH = 60;
const NAME_COLUMN_MIN_WIDTH = 120;

/**
 * Spreadsheet-style request list with multi-select and bulk actions.
 */
@Component({
  selector: 'app-requests-main-list',
  imports: [FormsModule, CdkVirtualScrollViewport, CdkVirtualForOf, CdkFixedSizeVirtualScroll, RequestBottomDrawer],
  templateUrl: './requests-main-list.html',
  styleUrl: './requests-main-list.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: {
    class: 'main-column',
    'aria-label': 'Requests',
    '(window:keydown)': 'onWindowKeydown($event)',
    '(window:mousemove)': 'onWindowMouseMove($event)',
    '(window:mouseup)': 'onWindowMouseUp()',
    '(window:resize)': 'onWindowResize()',
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

  protected readonly bottomTabs = BOTTOM_TABS;
  protected readonly httpMethods = HTTP_METHODS;
  protected readonly requestTags = this.tagApi.requestTags;
  protected readonly favouriteCollections = this.favouriteApi.collections;
  protected readonly favouriteMembership = this.favouriteApi.membership;
  protected readonly requestFavouriteIds = this.favouriteApi.requestsMembership;
  protected readonly collections = this.collectionApi.collections;

  readonly requestSearchQuery = signal('');
  readonly tagRequests = signal<HttpRequestSummary[]>([]);
  readonly recentRequests = signal<HttpRequestSummary[]>([]);
  readonly newRequestPopupOpen = signal(false);
  readonly newRequestName = signal('My new snappy API');
  readonly newRequestUrl = signal('');
  readonly newRequestMethod = signal('GET');

  // Selection state
  readonly selectedRowIds = computed(() => this.state.selectedIds());
  readonly selectedCell = signal<SelectedCell | null>(null);
  readonly highlightedCell = signal<SelectedCell | null>(null);
  readonly lastClickedRowId = signal<number | null>(null);
  readonly isDragging = signal(false);
  readonly dragStartRowId = signal<number | null>(null);
  readonly dragStartClientY = signal(0);
  readonly dragHasMoved = signal(false);
  readonly ignoreNextClick = signal(false);

  readonly sortColumn = signal<ColumnKey | null>(null);
  readonly sortDirection = signal<SortDirection>('asc');

  // ---------- View ordering capability ----------

  /** Which logical view (collection/favourite/tag/all/recent/bin) is shown. */
  readonly activeViewKind = computed<RequestListViewKind>(() => {
    if (this.state.showingBin()) return 'bin';
    if (this.state.showingFamousRequests()) return 'recent';
    if (this.state.selectedTag()) return 'tag';
    if (this.state.selectedFavouriteCollection()) return 'favourite';
    if (this.state.selectedCollection()) return 'collection';
    return 'all';
  });

  /**
   * Rows can only be drag-reordered on user-orderable views (see
   * request-view-ordering.ts) while the user's own order is actually the
   * displayed order: no column sort and no search filter active.
   */
  readonly canReorderRows = computed(
    () =>
      USER_ORDERABLE_VIEWS[this.activeViewKind()] &&
      this.sortColumn() === null &&
      !this.requestSearchQuery().trim(),
  );

  // ---------- Row reorder drag ----------

  private readonly reorderPending = signal<{ requestId: number; startY: number } | null>(null);
  /**
   * Structural drag state only (set once at start, cleared at end) so the
   * signal never changes per pixel. Per-cursor values live in plain fields
   * below and are applied to the DOM directly inside a rAF loop — writing a
   * signal per mousemove would trigger a change-detection pass per pixel.
   */
  readonly reorderDrag = signal<ReorderDrag | null>(null);
  private reorderInsertIndex = 0;
  private reorderLastX = 0;
  private reorderLastY = 0;
  private reorderFrame: number | null = null;
  private readonly reorderGhost = viewChild<ElementRef<HTMLElement>>('reorderGhost');
  private readonly reorderDropLine = viewChild<ElementRef<HTMLElement>>('reorderDropLine');

  /** Row briefly animated after a drop so the user sees where it landed. */
  readonly dropHighlightedId = signal<number | null>(null);
  private dropHighlightTimeout: ReturnType<typeof setTimeout> | null = null;

  protected readonly rowHeight = 44;
  /**  CDK buffer settings for smoother fast scrolling. */
  protected readonly virtualMinBufferPx = 200*10;
  protected readonly virtualMaxBufferPx = 400*10;

  readonly methodColumnWidth = signal(100);
  readonly nameColumnWidth = signal(200);
  private readonly columnResizeState = signal<{
    column: 'method' | 'name';
    startX: number;
    startWidth: number;
  } | null>(null);
  readonly isResizingColumn = computed(() => this.columnResizeState() !== null);
  readonly methodColumnWidthCss = computed(() => `${this.methodColumnWidth()}px`);
  readonly nameColumnWidthCss = computed(() => `${this.nameColumnWidth()}px`);

  readonly methodSeparatorLeft = signal(0);
  readonly nameSeparatorLeft = signal(0);
  private readonly resizeTick = signal(0);

  // Single-cell edit modals
  readonly editModalOpen = signal(false);
  readonly editModalColumn = signal<ColumnKey | null>(null);
  readonly editModalRequest = signal<HttpRequestSummary | null>(null);
  readonly editModalValue = signal('');

  readonly editingNameId = signal<number | null>(null);
  readonly editingNameValue = signal('');
  private readonly inlineNameInput = viewChild<ElementRef<HTMLInputElement>>('inlineNameInput');
  private readonly methodHeaderCell = viewChild<ElementRef<HTMLElement>>('methodHeaderCell');
  private readonly nameHeaderCell = viewChild<ElementRef<HTMLElement>>('nameHeaderCell');

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

  /**
   * Binned rows are structurally compatible view models except for the
   * ordering column (the bin is not user-orderable, so it is fixed at 0).
   */
  private binSummaries(): HttpRequestSummary[] {
    return this.binApi.binnedRequests().map(b => ({
      id: b.id,
      collection_id: b.collection_id,
      project_id: b.project_id,
      name: b.name,
      url: b.url,
      method: b.method,
      status_code: b.status_code,
      response_id: b.response_id,
      user_sort_order: 0,
    }));
  }

  readonly activeRequests = computed<HttpRequestSummary[]>(() => {
    if (this.state.showingBin()) {
      return this.binSummaries();
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

    if (this.state.showingFamousRequests()) {
      return this.recentRequests();
    }

    if (this.state.showingAllRequests()) {
      const projectId = this.state.selectedProject()?.id;
      return projectId ? this.apiRequests.forProject(projectId) : [];
    }

    return [];
  });

  readonly activeGroupName = computed<string | null>(() => {
    if (this.state.showingBin()) return 'Trash bin';
    if (this.state.showingFamousRequests()) return 'Recent';
    if (this.state.showingAllRequests()) return 'All requests';
    const tag = this.state.selectedTag();
    if (tag) return tag;
    const favourite = this.state.selectedFavouriteCollection();
    if (favourite) return favourite.name;
    const collection = this.state.selectedCollection();
    if (collection) return collection.name;
    return null;
  });

  readonly activeGroupKey = computed<string | null>(() => {
    const projectId = this.state.selectedProject()?.id ?? null;
    if (this.state.showingBin()) {
      return projectId !== null ? groupKey.bin(projectId) : null;
    }
    const tag = this.state.selectedTag();
    if (tag) {
      return projectId !== null ? groupKey.tag(projectId, tag) : null;
    }
    const favourite = this.state.selectedFavouriteCollection();
    if (favourite) {
      return groupKey.favourite(favourite.id);
    }
    const collection = this.state.selectedCollection();
    if (collection) {
      return groupKey.collection(collection.id);
    }
    if (this.state.showingFamousRequests()) {
      return projectId !== null ? groupKey.famous(projectId) : null;
    }
    if (this.state.showingAllRequests()) {
      return projectId !== null ? groupKey.allRequests(projectId) : null;
    }
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
    const cell = this.highlightedCell();
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
      const showFamous = this.state.showingFamousRequests();
      const showBin = this.state.showingBin();
      const project = this.state.selectedProject();

      const version = ++this.loadVersion;
      this.requestSearchQuery.set('');
      if (showFamous) {
        this.sortColumn.set(null);
        this.sortDirection.set('asc');
      }
      this.clearSelection();
      this.state.selectedRequest.set(null);
      this.state.selectedResponse.set(null);
      this.state.selectedBinnedRequest.set(null);
      this.state.multiSelectionActive.set(false);

      // Run the actual load outside the reactive context so that signal reads
      // inside it (selectedRequest, selectedCollection, etc.) do not become
      // dependencies of this effect. Otherwise changing selectedRequest later
      // (e.g. when loading the full request for send) would re-run the effect,
      // clear the selection, and scroll back to the remembered row.
      untracked(() => {
        void this.loadActiveGroup(
          version,
          collection?.id ?? null,
          favourite?.id ?? null,
          tag,
          showAll,
          showFamous,
          showBin,
          project?.id ?? null,
          this.activeGroupKey(),
        );
      });
    });

    effect(() => {
      this.state.multiSelectionActive.set(this.selectedRowIds().size > 1);
    });

    effect(() => {
      this.methodColumnWidth();
      this.nameColumnWidth();
      this.resizeTick();
      const methodCell = this.methodHeaderCell()?.nativeElement;
      const nameCell = this.nameHeaderCell()?.nativeElement;
      if (!methodCell || !nameCell) return;
      this.methodSeparatorLeft.set(methodCell.offsetLeft + methodCell.offsetWidth);
      this.nameSeparatorLeft.set(nameCell.offsetLeft + nameCell.offsetWidth);
    });
  }

  private async loadActiveGroup(
    version: number,
    collectionId: number | null,
    favouriteId: number | null,
    tag: string | null,
    showAll: boolean,
    showFamous: boolean,
    showBin: boolean,
    projectId: number | null,
    groupKeyOfView: string | null,
  ): Promise<void> {
    try {
      if (showBin && projectId !== null) {
        await this.binApi.loadForProject(projectId);
        if (version !== this.loadVersion) return;
        this.restoreRememberedRequest(
          this.binSummaries(),
          this.rememberedRequestId(groupKeyOfView),
        );
        return;
      }

      if (showFamous && projectId !== null) {
        const requests = await this.requestApi.loadRecentRequests(projectId, 50);
        if (version !== this.loadVersion) return;
        this.recentRequests.set(requests);
        this.restoreRememberedRequest(requests, this.rememberedRequestId(groupKeyOfView));
        return;
      }

      if (tag) {
        const project = this.state.selectedProject();
        if (!project) return;
        const requests = await this.tagApi.getRequestsForTag(project.id, tag);
        if (version !== this.loadVersion) return;
        this.tagRequests.set(requests);
        await this.tagApi.loadTagsForRequests(requests);
        if (version !== this.loadVersion) return;
        this.favouriteApi.requestsMembership.set({});
        this.restoreRememberedRequest(requests, this.rememberedRequestId(groupKeyOfView));
        return;
      }

      if (favouriteId !== null) {
        await this.favouriteApi.loadRequestsForCollection(favouriteId);
        if (version !== this.loadVersion) return;
        const requests = this.favouriteApi.requests();
        await this.tagApi.loadTagsForRequests(requests);
        if (version !== this.loadVersion) return;
        this.restoreRememberedRequest(requests, this.rememberedRequestId(groupKeyOfView));
        return;
      }

      if (collectionId !== null) {
        if (version !== this.loadVersion) return;
        const requests = this.apiRequests.forCollection(collectionId);
        await this.tagApi.loadTagsForRequests(requests);
        if (version !== this.loadVersion) return;
        this.restoreRememberedRequest(requests, this.rememberedRequestId(groupKeyOfView));
        return;
      }

      if (showAll && projectId !== null) {
        if (version !== this.loadVersion) return;
        const requests = this.apiRequests.forProject(projectId);
        await this.tagApi.loadTagsForRequests(requests);
        if (version !== this.loadVersion) return;
        this.restoreRememberedRequest(requests, this.rememberedRequestId(groupKeyOfView));
        return;
      }

      this.tagRequests.set([]);
      this.favouriteApi.requestsMembership.set({});
    } catch (err) {
      console.error(err);
    }
  }

  private rememberedRequestId(groupKeyOfView: string | null): number | null {
    return groupKeyOfView !== null ? this.selectionState.getSelectedRequest(groupKeyOfView) : null;
  }

  private restoreRememberedRequest(requests: HttpRequestSummary[], rememberedId: number | null): void {
    if (rememberedId === null) return;
    const remembered = requests.find((r) => r.id === rememberedId);
    if (!remembered) return;

    // Restore selection without letting the outer reactive effect track the
    // workspace request signal. Otherwise any later change to selectedRequest
    // (e.g. loading the full request for send) would re-run the group-load
    // effect, reset the selection, and scroll the list back to the remembered row.
    untracked(() => {
      this.selectCell(remembered, 'name');
    });

    // Ensure the remembered row is visible in the virtual viewport. The index
    // must come from the displayed list so active sorting/search is respected.
    if (this.viewport) {
      const index = this.filteredActiveRequests().findIndex((r) => r.id === rememberedId);
      if (index >= 0) {
        this.viewport.scrollToIndex(index, 'auto');
      }
    }
  }

  onWindowKeydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      // Cancelling an in-progress reorder takes precedence: no order changes.
      if (this.cancelReorder()) return;
      this.onEscapePressed();
      return;
    }

    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'd') {
      event.preventDefault();
      if (this.state.showingBin()) return;
      void this.duplicateSelectedRequest();
      return;
    }

    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 's') {
      event.preventDefault();
      if (this.state.showingBin()) return;
      void this.sendSelectedRequest(event);
    }
  }

  onEscapePressed(): void {
    if (this.editingNameId() !== null) {
      this.cancelInlineNameEdit();
      return;
    }
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

  selectCell(
    req: HttpRequestSummary | HttpBinnedRequestSummary,
    column: ColumnKey,
    highlight = true,
  ): void {
    this.state.selectedIds.set(new Set([req.id]));
    const cell = { requestId: req.id, column };
    this.selectedCell.set(cell);
    this.highlightedCell.set(highlight ? cell : null);
    this.syncWorkspaceSelection(req);

    const groupKeyOfView = this.activeGroupKey();
    if (groupKeyOfView !== null) {
      this.selectionState.setSelectedRequest(groupKeyOfView, req.id);
    }
  }

  private clearSelection(): void {
    this.state.selectedIds.set(new Set());
    this.selectedCell.set(null);
    this.highlightedCell.set(null);
    this.cancelInlineNameEdit();
  }

  trackById(_index: number, req: HttpRequestSummary): number {
    return req.id;
  }

  onRowClick(
    req: HttpRequestSummary | HttpBinnedRequestSummary,
    event: MouseEvent,
    column: ColumnKey,
  ): void {
    if (event.button !== 0) return;
    event.stopPropagation();
    this.contextMenu.close();

    if (this.ignoreNextClick()) {
      this.ignoreNextClick.set(false);
      return;
    }

    if (event.shiftKey) {
      this.selectRangeTo(req, column);
      return;
    }

    if (event.ctrlKey || event.metaKey) {
      this.toggleRowSelection(req, column);
      return;
    }

    // Plain click: clear existing multi-selection and select this row only.
    // The dashed cell outline only appears when the click hits the cell content.
    const onContent =
      event.target instanceof Element &&
      !!event.target.closest('.cell-text, .request-method');
    this.selectCell(req, column, onContent);
    this.lastClickedRowId.set(req.id);
  }

  private toggleRowSelection(req: HttpRequestSummary | HttpBinnedRequestSummary, column: ColumnKey): void {
    const set = new Set(this.selectedRowIds());
    if (set.has(req.id)) {
      set.delete(req.id);
    } else {
      set.add(req.id);
    }
    this.state.selectedIds.set(set);
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

    const current = untracked(() => this.state.selectedRequest());
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
    this.state.selectedIds.set(next);
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
    if (event.ctrlKey || event.metaKey || event.shiftKey) return;

    this.dragStartRowId.set(req.id);
    this.dragStartClientY.set(event.clientY);
    this.dragHasMoved.set(false);
    this.isDragging.set(false);
  }

  onWindowMouseMove(event: MouseEvent): void {
    const resizeState = this.columnResizeState();
    if (resizeState) {
      const delta = event.clientX - resizeState.startX;
      const minWidth =
        resizeState.column === 'method' ? METHOD_COLUMN_MIN_WIDTH : NAME_COLUMN_MIN_WIDTH;
      const nextWidth = Math.max(minWidth, resizeState.startWidth + delta);
      if (resizeState.column === 'method') {
        this.methodColumnWidth.set(nextWidth);
      } else {
        this.nameColumnWidth.set(nextWidth);
      }
      return;
    }

    if (this.reorderPending() !== null || this.reorderDrag() !== null) {
      this.handleReorderMouseMove(event);
      return;
    }

    if (this.dragStartRowId() === null) return;

    if (!this.isDragging()) {
      const delta = Math.abs(event.clientY - this.dragStartClientY());
      if (delta > DRAG_THRESHOLD_PX) {
        this.isDragging.set(true);
        this.dragHasMoved.set(true);
        const startReq = this.activeRequests().find((r) => r.id === this.dragStartRowId());
        if (startReq) {
          this.state.selectedIds.set(new Set([startReq.id]));
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
   * Dividing that by the fixed row height (must match the viewport's
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
    const rowHeight = this.rowHeight;
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

    this.state.selectedIds.set(next);
    this.selectedCell.set({ requestId: currentReq.id, column: 'name' });
  }

  onWindowMouseUp(): void {
    if (this.reorderPending() !== null || this.reorderDrag() !== null) {
      const drag = this.reorderDrag();
      this.reorderPending.set(null);
      this.reorderDrag.set(null);
      this.stopReorderFrameLoop();
      document.body.classList.remove('reorder-dragging');
      if (drag) {
        this.ignoreNextClick.set(true);
        // The insertion slot counts the dragged row itself; adjust for its
        // removal to get the final insert position.
        const toIndex =
          this.reorderInsertIndex > drag.fromIndex ? this.reorderInsertIndex - 1 : this.reorderInsertIndex;
        if (toIndex !== drag.fromIndex) {
          this.commitReorder(drag.fromIndex, toIndex);
          this.flashDroppedRow(drag.request.id);
          void this.persistReorder();
        }
      }
      return;
    }

    if (this.columnResizeState()) {
      this.columnResizeState.set(null);
      return;
    }

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

  // ---------- Row reorder drag ----------

  onReorderHandleMouseDown(req: HttpRequestSummary, event: MouseEvent): void {
    if (event.button !== 0) return;
    // Keep the gesture away from row clicks, the multi-select drag and text
    // selection: the handle owns this mousedown.
    event.preventDefault();
    event.stopPropagation();
    // Defense in depth: the handle is hidden in multi-selection for selected
    // rows, but re-check so a stale/hidden handle can never start a drag on
    // one of the selected members.
    if (!this.canReorderRows()) return;
    if (this.selectedRowIds().size > 1 && this.selectedRowIds().has(req.id)) return;
    this.reorderPending.set({ requestId: req.id, startY: event.clientY });
  }

  /**
   * Reorder drag powered by the same arithmetic-index technique as drag
   * selection: recycled virtual rows are never inspected, the insertion slot
   * is derived from (clientY - viewport top + scrollOffset) / rowHeight.
   *
   * The list itself is NOT reordered while dragging. A ghost of the source
   * row follows the cursor with a drop indicator between rows; the order is
   * only committed (in memory + database) on release. ESC cancels.
   *
   * Per-pixel updates bypass Angular entirely: mousemove only records plain
   * fields, and a requestAnimationFrame loop applies auto-scroll, geometry
   * and the ghost/indicator positions via direct DOM writes.
   */
  private handleReorderMouseMove(event: MouseEvent): void {
    const pending = this.reorderPending();
    if (pending) {
      if (Math.abs(event.clientY - pending.startY) <= DRAG_THRESHOLD_PX) return;
      const requests = this.filteredActiveRequests();
      const fromIndex = requests.findIndex(r => r.id === pending.requestId);
      this.reorderPending.set(null);
      if (fromIndex === -1 || !this.canReorderRows()) return;
      this.reorderLastX = event.clientX;
      this.reorderLastY = event.clientY;
      this.reorderInsertIndex = fromIndex;
      this.reorderDrag.set({ request: requests[fromIndex], fromIndex });
      this.startReorderFrameLoop();
      // A body-level class (global CSS forces the grabbing cursor over every
      // element) — element-level cursor rules would otherwise win as the
      // pointer crosses buttons and other rows' handles.
      document.body.classList.add('reorder-dragging');
      return;
    }

    if (!this.reorderDrag()) return;
    this.reorderLastX = event.clientX;
    this.reorderLastY = event.clientY;
  }

  /**
   * One frame of the drag: edge auto-scroll, insertion slot computation and
   * direct DOM writes for the ghost and drop indicator. Runs at display
   * refresh rate; auto-scroll keeps going even when the cursor is held
   * still near an edge.
   */
  private renderReorderFrame(): void {
    const drag = this.reorderDrag();
    const ghost = this.reorderGhost()?.nativeElement;
    const line = this.reorderDropLine()?.nativeElement;
    if (!drag || !ghost || !line || !this.viewport) return;

    const viewport = this.viewport.elementRef.nativeElement;
    const rect = viewport.getBoundingClientRect();

    if (this.reorderLastY < rect.top + REORDER_EDGE_SCROLL_PX) {
      this.viewport.scrollToOffset(
        Math.max(0, this.viewport.measureScrollOffset('top') - REORDER_EDGE_SCROLL_STEP_PX),
      );
    } else if (this.reorderLastY > rect.bottom - REORDER_EDGE_SCROLL_PX) {
      // Native scrollTop clamping stops at the end of the content.
      this.viewport.scrollToOffset(
        this.viewport.measureScrollOffset('top') + REORDER_EDGE_SCROLL_STEP_PX,
      );
    }

    const scrollOffset = this.viewport.measureScrollOffset('top');
    const length = this.filteredActiveRequests().length;

    // Insertion slot between rows: the top half of a row inserts above it,
    // the bottom half below it; clamped to the list ends.
    const yInContent = scrollOffset + this.reorderLastY - rect.top;
    this.reorderInsertIndex = Math.max(
      0,
      Math.min(Math.round(yInContent / this.rowHeight), length),
    );

    const lineTop = rect.top + this.reorderInsertIndex * this.rowHeight - scrollOffset;
    ghost.style.left = `${this.reorderLastX}px`;
    ghost.style.top = `${this.reorderLastY}px`;
    line.style.top = `${lineTop}px`;
    line.style.left = `${rect.left + 4}px`;
    line.style.width = `${Math.max(0, rect.width - 8)}px`;
    // Hidden by CSS until first positioned, so the ghost never flashes at
    // its unpositioned coordinates.
    ghost.style.visibility = 'visible';
    line.style.visibility = 'visible';
  }

  private startReorderFrameLoop(): void {
    if (this.reorderFrame !== null) return;
    const tick = (): void => {
      this.reorderFrame = null;
      if (this.reorderDrag() === null) return;
      this.renderReorderFrame();
      this.reorderFrame = requestAnimationFrame(tick);
    };
    this.reorderFrame = requestAnimationFrame(tick);
  }

  private stopReorderFrameLoop(): void {
    if (this.reorderFrame !== null) {
      cancelAnimationFrame(this.reorderFrame);
      this.reorderFrame = null;
    }
  }

  /**
   * Plays the drop-confirmation bounce on the moved row. The id is cleared
   * and re-applied on the next frame so dropping the same row again restarts
   * the animation instead of reusing the already-present class.
   */
  private flashDroppedRow(requestId: number): void {
    if (this.dropHighlightTimeout !== null) {
      clearTimeout(this.dropHighlightTimeout);
      this.dropHighlightTimeout = null;
    }
    this.dropHighlightedId.set(null);
    requestAnimationFrame(() => {
      this.dropHighlightedId.set(requestId);
      this.dropHighlightTimeout = setTimeout(() => {
        this.dropHighlightedId.set(null);
        this.dropHighlightTimeout = null;
      }, DROP_HIGHLIGHT_MS);
    });
  }

  /** Aborts an in-progress reorder gesture without changing any order. */
  private cancelReorder(): boolean {
    if (this.reorderPending() === null && this.reorderDrag() === null) return false;
    this.reorderPending.set(null);
    this.reorderDrag.set(null);
    this.stopReorderFrameLoop();
    document.body.classList.remove('reorder-dragging');
    return true;
  }

  ngOnDestroy(): void {
    // If the component goes away mid-drag (view switch), the window-level
    // mouseup listener dies with it — clean up here so the global dragging
    // cursor and the rAF loop never leak.
    this.cancelReorder();
    if (this.dropHighlightTimeout !== null) {
      clearTimeout(this.dropHighlightTimeout);
      this.dropHighlightTimeout = null;
    }
  }

  /**
   * Commits the drop: moves the row in the active view's source and assigns
   * user_sort_order 0..n-1 in ONE bulk update per store. `to` is the insert
   * position in the list *after* the dragged row has been removed, so
   * callers adjust the between-rows insertion slot beforehand.
   */
  private commitReorder(from: number, to: number): void {
    const kind = this.activeViewKind();

    if (kind === 'collection') {
      const collection = this.state.selectedCollection();
      if (!collection) return;
      const next = [...this.apiRequests.forCollection(collection.id)];
      const [moved] = next.splice(from, 1);
      if (!moved) return;
      next.splice(to, 0, moved);
      const orders: Record<number, number> = {};
      for (const [index, req] of next.entries()) {
        orders[req.id] = index;
      }
      this.apiRequests.applyUserSortOrders(orders);
      return;
    }

    if (kind === 'favourite') {
      const next = [...this.favouriteApi.requests()];
      const [moved] = next.splice(from, 1);
      if (!moved) return;
      next.splice(to, 0, moved);
      this.favouriteApi.requests.set(next.map((req, index) => ({ ...req, user_sort_order: index })));
      return;
    }

    if (kind === 'tag') {
      const next = [...this.tagRequests()];
      const [moved] = next.splice(from, 1);
      if (!moved) return;
      next.splice(to, 0, moved);
      this.tagRequests.set(next.map((req, index) => ({ ...req, user_sort_order: index })));
    }
  }

  /** Persists the displayed order; on failure the view is reloaded from DB. */
  private async persistReorder(): Promise<void> {
    const orderedIds = this.filteredActiveRequests().map(r => r.id);
    if (orderedIds.length === 0) return;

    const kind = this.activeViewKind();
    try {
      if (kind === 'collection') {
        const collection = this.state.selectedCollection();
        if (!collection) return;
        await this.requestApi.updateCollectionRequestOrder(collection.id, orderedIds);
      } else if (kind === 'favourite') {
        const favourite = this.state.selectedFavouriteCollection();
        if (!favourite) return;
        await this.favouriteApi.updateRequestOrder(favourite.id, orderedIds);
      } else if (kind === 'tag') {
        const project = this.state.selectedProject();
        const tag = this.state.selectedTag();
        if (!project || !tag) return;
        await this.tagApi.updateRequestOrder(project.id, tag, orderedIds);
      }
    } catch (err) {
      console.error('persisting request order failed, reloading view', err);
      try {
        if (kind === 'collection') {
          await this.apiRequests.loadAll();
        } else if (kind === 'favourite') {
          const favourite = this.state.selectedFavouriteCollection();
          if (favourite) await this.favouriteApi.loadRequestsForCollection(favourite.id);
        } else if (kind === 'tag') {
          const project = this.state.selectedProject();
          const tag = this.state.selectedTag();
          if (project && tag) {
            this.tagRequests.set(await this.tagApi.getRequestsForTag(project.id, tag));
          }
        }
      } catch (reloadErr) {
        console.error('reloading view after failed reorder', reloadErr);
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
      await Clipboard.SetText(curl);
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

  /**
   * Column sort cycles asc -> desc -> none. The third click clears the sort
   * so the list returns to the user's custom order (or the view's natural
   * order on non-orderable views).
   */
  toggleSort(column: ColumnKey): void {
    if (this.sortColumn() === column) {
      if (this.sortDirection() === 'asc') {
        this.sortDirection.set('desc');
      } else {
        this.sortColumn.set(null);
        this.sortDirection.set('asc');
      }
    } else {
      this.sortColumn.set(column);
      this.sortDirection.set('asc');
    }
  }

  startColumnResize(event: MouseEvent, column: 'method' | 'name'): void {
    event.preventDefault();
    event.stopPropagation();
    this.columnResizeState.set({
      column,
      startX: event.clientX,
      startWidth: column === 'method' ? this.methodColumnWidth() : this.nameColumnWidth(),
    });
  }

  onWindowResize(): void {
    this.resizeTick.update((tick) => tick + 1);
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
    this.newRequestName.set(mode === 'curl' ? '' : 'My new snappy API');
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
        body_type: 'json',
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
      const name = this.newRequestName().trim() || req.name;
      await this.apiRequests.create({
        collection_id: collectionId,
        project_id: projectId,
        name,
        url: req.url,
        method: req.method,
        body: req.body,
        body_type: req.body_type || 'json',
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

  async sendSelectedRequest(event: Event): Promise<void> {
    if (this.state.showingBin()) return;

    const selectedIds = [...this.selectedRowIds()];
    if (selectedIds.length > 1) {
      await this.sendMultipleRequests(selectedIds, event);
      return;
    }

    let req = this.state.selectedRequest();
    if (!req) {
      const selectedId = this.selectedCell()?.requestId ?? selectedIds[0];
      if (!selectedId) return;
      try {
        req = await this.requestApi.get(selectedId);
      } catch (err) {
        console.error(err);
        return;
      }
      this.state.selectedRequest.set(req);
    }
    await this.state.sendRequest(req, event, !this.state.showingFamousRequests());
  }

  private async sendMultipleRequests(ids: number[], event: Event): Promise<void> {
    try {
      const requests = await Promise.all(ids.map((id) => this.requestApi.get(id)));
      await this.state.sendRequests(requests, event, !this.state.showingFamousRequests());
    } catch (err) {
      console.error(err);
    }
  }

  async sendRowRequest(
    req: HttpRequestSummary | HttpBinnedRequestSummary,
    event: Event,
  ): Promise<void> {
    event.stopPropagation();
    if (this.state.showingBin()) return;

    this.contextMenu.close();
    const selected = this.selectedRowIds();
    if (selected.size <= 1 || !selected.has(req.id)) {
      this.selectCell(req as HttpRequestSummary, 'name');
      this.lastClickedRowId.set(req.id);
    }
    await this.sendSelectedRequest(event);
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
    if (column === 'name') {
      this.startInlineNameEdit(request);
      return;
    }
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

  // ---------- Inline name edit ----------

  startInlineNameEdit(req: HttpRequestSummary): void {
    this.contextMenu.close();
    this.editingNameId.set(req.id);
    this.editingNameValue.set(req.name);

    setTimeout(() => {
      const input = this.inlineNameInput()?.nativeElement;
      input?.focus();
      input?.select();
    });
  }

  cancelInlineNameEdit(): void {
    this.editingNameId.set(null);
    this.editingNameValue.set('');
  }

  async commitInlineNameEdit(req: HttpRequestSummary): Promise<void> {
    if (this.editingNameId() !== req.id) return;

    const value = this.editingNameValue();
    this.cancelInlineNameEdit();
    if (value === req.name) return;

    await this.updateRequestField(req, 'name', value);
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
