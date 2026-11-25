import { ChangeDetectionStrategy, Component, DestroyRef, ElementRef, Injector, OnDestroy, TemplateRef, ViewChild, afterNextRender, afterRenderEffect, computed, effect, inject, linkedSignal, signal, untracked, viewChild } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { CdkFixedSizeVirtualScroll, CdkVirtualForOf, CdkVirtualScrollViewport } from '@angular/cdk/scrolling';
import { Clipboard } from '@wailsio/runtime';
import { WorkspaceStateService } from '../core/services/workspace-state.service';
import { ChangeBus } from '../core/services/change-bus.service';
import { RequestApiService, type HttpRequest, type HttpRequestSummary } from '../core/services/request.service';
import { HTTP_METHODS } from '../core/models/http';
import { FavouriteApiService } from '../core/services/favourite.service';
import { CollectionApiService, type Collection } from '../core/services/collection.service';
import { SelectionStateService, groupKey } from '../core/services/selection-state.service';
import { ScriptApiService } from '../core/services/script.service';
import { ScriptPickerDialog } from '../dialogs/script-picker-dialog/script-picker-dialog';
import { FavouritePickerDialog } from '../dialogs/favourite-picker-dialog/favourite-picker-dialog';
import { RestoreToCollectionDialog } from '../dialogs/restore-to-collection-dialog/restore-to-collection-dialog';
import { CurlImportDialog } from '../dialogs/curl-import-dialog/curl-import-dialog';
import { SendButton } from '../send-button/send-button';
import { UsageMeter } from '../usage-meter/usage-meter';
import { LastUsedPaw } from '../last-used-paw/last-used-paw';
import { buildSearchIndex, filterRequests, queryWords, rankRequests } from '../core/request-search';
import { DragSessionService } from '../core/services/drag-session.service';
import { BinApiService, type HttpBinnedRequestSummary } from '../core/services/bin.service';
import { ContextMenuService } from '../core/services/context-menu.service';
import { ConfirmService } from '../core/services/confirm.service';
import { isEditableTarget } from '../core/dom/editable-target';
import { RequestBottomDrawer, type BottomTab } from '../request-bottom-drawer/request-bottom-drawer';
import { RequestBodyTab } from '../request-body-tab/request-body-tab';
import { ScriptTabs } from '../script-tabs/script-tabs';
import { RequestHeadersTab } from '../request-headers-tab/request-headers-tab';
import { CodeEditor } from '../code-editor/code-editor';
import { Select, type SelectOption } from '../select/select';
import { SelectOnFocusDirective } from '../core/directives/select-on-focus.directive';
import { requestColorInfo } from '../core/request-colors';
import type { WorkspaceViewKind } from '../core/workspace/workspace-view';
import type { RequestGroup } from '../core/workspace/presorted';

type ColumnKey = 'name' | 'url' | 'method' | 'scripts';
type SortDirection = 'asc' | 'desc';

/**
 * One drawn row of the list. Smart views draw their groups' headings as rows
 * too (same height, so virtual scrolling still works); the other views draw
 * request rows only.
 */
type DisplayRow =
  | { kind: 'heading'; key: string; label: string; count: number }
  | { kind: 'request'; req: HttpRequestSummary };

interface SelectedCell {
  requestId: number;
  column: ColumnKey;
}

interface RowState {
  isSelected: boolean;
  isCellSelected: Record<ColumnKey, boolean>;
  /** URL with environment variables resolved, for display only. */
  displayUrl: string;
  /** User colour (hex) for this row in the current view, or null. */
  colorVar: string | null;
  /** Row background tint matching colorVar, or null. */
  colorTint: string | null;
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
  imports: [FormsModule, CdkVirtualScrollViewport, CdkVirtualForOf, CdkFixedSizeVirtualScroll, RequestBottomDrawer, CodeEditor, Select, SelectOnFocusDirective, ScriptTabs, ScriptPickerDialog, FavouritePickerDialog, RestoreToCollectionDialog, CurlImportDialog, SendButton, UsageMeter, LastUsedPaw],
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
  private readonly changeBus = inject(ChangeBus);
  private readonly favouriteApi = inject(FavouriteApiService);
  private readonly collectionApi = inject(CollectionApiService);
  private readonly selectionState = inject(SelectionStateService);
  private readonly scriptApi = inject(ScriptApiService);
  private readonly binApi = inject(BinApiService);
  private readonly injector = inject(Injector);
  private readonly confirm = inject(ConfirmService);
  private readonly contextMenu = inject(ContextMenuService);
  private readonly dragSession = inject(DragSessionService);

  @ViewChild(CdkVirtualScrollViewport, { static: false })
  private readonly viewport!: CdkVirtualScrollViewport;

  protected readonly bottomTabs = BOTTOM_TABS;
  protected readonly methodOptions: SelectOption[] = HTTP_METHODS.map((method) => ({ value: method, label: method }));
  protected readonly requestScripts = this.scriptApi.requestScripts;
  protected readonly requestFavouriteIds = this.favouriteApi.requestsMembership;
  protected readonly collections = this.collectionApi.collections;

  readonly requestSearchQuery = this.state.requestSearchQuery;
  readonly scriptRequests = signal<HttpRequestSummary[]>([]);
  /** The collection the cURL import dialog is open for, or null when it is closed. */
  readonly curlImportFor = signal<Collection | null>(null);

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
  /** Smart views keep their own order: their column headers don't sort. */
  readonly sortable = computed(() => this.state.view()?.sortable ?? true);

  /** How much each request is used lately (the Usage column's levels). */
  protected readonly usage = this.requestApi.usage;
  /** The Usage column shows in the Most used view only. */
  readonly showUsage = computed(() => this.state.smartView()?.kind === 'usage');

  /** Ticks each minute, so the smart views' day headings and "30+ days" stay true. */
  private readonly now = signal(Date.now());

  /** The open smart view's groups (null in the other views). */
  readonly smartGroups = computed<RequestGroup[] | null>(() => {
    const view = this.state.smartView();
    const projectId = this.state.selectedProject()?.id;
    if (!view || projectId === undefined) return null;
    return view.arrange(this.requestApi.forProject(projectId), {
      now: this.now(),
      usage: this.requestApi.usage(),
    });
  });

  // ---------- View ordering capability ----------

  /** Which view is shown; "all" when none is open. */
  readonly activeViewKind = computed<WorkspaceViewKind>(() => this.state.view()?.kind ?? 'all');

  /**
   * Rows can only be drag-reordered in views that allow it while the user's own order is actually the
   * displayed order: no column sort and no search filter active.
   */
  readonly canReorderRows = computed(
    () =>
      (this.state.view()?.canReorder ?? false) &&
      this.sortColumn() === null &&
      !this.requestSearchQuery().trim(),
  );

  /** The bin, All requests and the smart views have a fixed order and no drag handle. */
  readonly canDragOut = computed(
    () => !this.state.showingBin() && !this.state.smartView() && this.activeViewKind() !== 'all',
  );

  /** Whether the colour picker is available (selection in a colour-able view). */
  readonly canColorSelection = computed(
    () => (this.state.view()?.canColour ?? false) && this.selectedRowIds().size > 0,
  );

  /** The colour shared by all selected rows in this view, or null when mixed/none. */
  readonly activeColor = computed<string | null>(() => {
    const selected = this.selectedRowIds();
    if (selected.size === 0) return null;
    let first: string | null | undefined;
    for (const req of this.filteredActiveRequests()) {
      if (!selected.has(req.id)) continue;
      if (first === undefined) {
        first = req.user_color;
        continue;
      }
      if (first !== req.user_color) return null;
    }
    return first ?? null;
  });

  // ---------- Row reorder drag ----------

  private readonly reorderPending = signal<{
    requestId: number;
    startY: number;
    externalOnly: boolean;
    request: HttpRequestSummary;
  } | null>(null);
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

  protected readonly rowHeight = 38;
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
  protected readonly resizingColumn = computed(() => this.columnResizeState()?.column ?? null);
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
  private readonly methodHeaderCell = viewChild<ElementRef<HTMLElement>>('methodHeaderCell');
  private readonly nameHeaderCell = viewChild<ElementRef<HTMLElement>>('nameHeaderCell');


  /** The requests the script picker is open for, or null when it is closed. */
  readonly scriptPickerFor = signal<HttpRequestSummary[] | null>(null);

  /** The requests the favourite picker is open for, or null when it is closed. */
  readonly favouritePickerFor = signal<HttpRequestSummary[] | null>(null);


  readonly moveCollectionModalOpen = signal(false);

  // Restore collection picker (used when the original collection is gone).
  /** Bin ids the restore dialog is open for, or null when it is closed. */
  readonly restorePickerFor = signal<number[] | null>(null);

  private loadVersion = 0;

  /** Remembered scroll offset per view (group key), restored on view switch. */
  private readonly scrollOffsets = new Map<string, number>();
  private lastGroupKey: string | null = null;

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
      user_color: null,
      last_used_at: 0,
    }));
  }

  readonly activeRequests = computed<HttpRequestSummary[]>(() => {
    if (this.state.showingBin()) {
      return this.binSummaries();
    }

    const script = this.state.selectedScript();
    if (script) {
      return this.scriptRequests();
    }

    if (this.state.selectedFavouriteCollection()) {
      return this.favouriteApi.requests();
    }

    const collection = this.state.selectedCollection();
    if (collection) {
      return this.requestApi.forCollection(collection.id);
    }

    const groups = this.smartGroups();
    if (groups) {
      return groups.flatMap((g) => g.requests);
    }

    if (this.state.showingAllRequests()) {
      const projectId = this.state.selectedProject()?.id;
      return projectId ? this.requestApi.forProject(projectId) : [];
    }

    return [];
  });

  readonly activeGroupName = computed<string | null>(() => this.state.view()?.title ?? null);

  readonly activeGroupKey = computed<string | null>(() => {
    const projectId = this.state.selectedProject()?.id ?? null;
    if (this.state.showingBin()) {
      return projectId !== null ? groupKey.bin(projectId) : null;
    }
    const script = this.state.selectedScript();
    if (script) {
      return projectId !== null ? groupKey.script(projectId, script) : null;
    }
    const favourite = this.state.selectedFavouriteCollection();
    if (favourite) {
      return groupKey.favourite(favourite.id);
    }
    const collection = this.state.selectedCollection();
    if (collection) {
      return groupKey.collection(collection.id);
    }
    const smart = this.state.smartView();
    if (smart) {
      return projectId !== null ? groupKey.smart(projectId, smart.kind) : null;
    }
    if (this.state.showingAllRequests()) {
      return projectId !== null ? groupKey.allRequests(projectId) : null;
    }
    return null;
  });

  /** The search's words (empty when the search box is). */
  private readonly searchWords = computed(() => queryWords(this.requestSearchQuery()));

  /** The current list prepared for searching; rebuilt when the list changes, not per keystroke. */
  private readonly searchIndex = computed(() => {
    const names = new Map(this.collectionApi.collections().map((c) => [c.id, c.name]));
    return buildSearchIndex(this.activeRequests(), (id) => names.get(id));
  });

  /** A smart view's groups narrowed by the search (keeping their order), or null. */
  readonly filteredGroups = computed<RequestGroup[] | null>(() => {
    const groups = this.smartGroups();
    if (!groups) return null;
    const words = this.searchWords();
    if (!words.length) return groups;
    const matching = new Set(filterRequests(this.searchIndex(), words).map((r) => r.id));
    return groups
      .map((g) => ({ ...g, requests: g.requests.filter((req) => matching.has(req.id)) }))
      .filter((g) => g.requests.length > 0);
  });

  /**
   * The rows shown. Searching ranks them (best match, then recent usage), unless
   * a column sort is chosen: then it only filters and the column decides.
   */
  readonly filteredActiveRequests = computed<HttpRequestSummary[]>(() => {
    const groups = this.filteredGroups();
    if (groups) {
      return groups.flatMap((g) => g.requests);
    }

    const words = this.searchWords();
    let result = this.activeRequests();

    if (words.length) {
      if (!this.sortColumn()) {
        return rankRequests(this.searchIndex(), words, this.requestApi.usage());
      }
      result = filterRequests(this.searchIndex(), words);
    }

    const sortCol = this.sortColumn();
    if (sortCol) {
      result = [...result].sort((a, b) => this.compareRequests(a, b, sortCol));
    }

    return result;
  });

  /** What the list draws: request rows, plus a smart view's group headings. */
  readonly displayRows = computed<DisplayRow[]>(() => {
    const groups = this.filteredGroups();
    if (!groups) {
      return this.filteredActiveRequests().map((req): DisplayRow => ({ kind: 'request', req }));
    }
    const rows: DisplayRow[] = [];
    for (const group of groups) {
      rows.push({ kind: 'heading', key: group.key, label: group.label, count: group.requests.length });
      for (const req of group.requests) rows.push({ kind: 'request', req });
    }
    return rows;
  });

  /** The most recently sent of the drawn requests, if any was sent. */
  protected readonly lastUsedRequestId = computed(() => {
    let id: number | null = null;
    let latest = 0;
    for (const row of this.displayRows()) {
      if (row.kind === 'request' && row.req.last_used_at > latest) {
        latest = row.req.last_used_at;
        id = row.req.id;
      }
    }
    return id;
  });

  /** A request's position among the drawn rows (headings included), for scrolling. */
  private readonly displayIndexById = computed(() => {
    const index = new Map<number, number>();
    this.displayRows().forEach((row, i) => {
      if (row.kind === 'request') index.set(row.req.id, i);
    });
    return index;
  });

  protected trackRow(_: number, row: DisplayRow): string | number {
    return row.kind === 'heading' ? `heading:${row.key}` : row.req.id;
  }

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
    const environment = this.state.environment();
    const result: Record<number, RowState> = {};

    for (const req of this.filteredActiveRequests()) {
      const isSelected = selectedIds.has(req.id);
      // The dashed cell outline is only shown for single-row selection; when
      // multiple rows are selected the outline would be meaningless noise.
      const isCellSelected: Record<ColumnKey, boolean> = {
        name: false,
        url: false,
        method: false,
        scripts: false,
      };
      if (isSelected && selectedIds.size <= 1 && cell !== null && cell.requestId === req.id) {
        isCellSelected[cell.column] = true;
      }
      const colour = requestColorInfo(req.user_color);
      result[req.id] = {
        isSelected,
        isCellSelected,
        displayUrl: environment.interpolate(req.url),
        colorVar: colour?.cssVar ?? null,
        colorTint: colour?.tint ?? null,
      };
    }
    return result;
  });

  readonly selectedRequest = computed<HttpRequestSummary | null>(() => {
    const cell = this.selectedCell();
    if (!cell) return null;
    return this.activeRequests().find((r) => r.id === cell.requestId) ?? null;
  });

  /** The request whose URL the header edits: exactly one selected, outside the bin. */
  protected readonly headerRequest = computed(() =>
    !this.state.showingBin() && this.selectedRowIds().size === 1 ? this.selectedRequest() : null,
  );
  protected readonly showHeader = computed(
    () => !this.state.showingBin() && this.selectedRowIds().size > 0,
  );
  // Reset only when another request is selected: the saved URL coming back
  // mid-typing would otherwise overwrite what was typed since.
  protected readonly urlDraft = linkedSignal<number | null, string>({
    source: () => this.headerRequest()?.id ?? null,
    computation: () => untracked(() => this.headerRequest()?.url ?? ''),
  });
  private urlSaveTimer: ReturnType<typeof setTimeout> | undefined;

  protected onUrlChange(url: string): void {
    this.urlDraft.set(url);
    clearTimeout(this.urlSaveTimer);
    const req = this.headerRequest();
    if (req) this.urlSaveTimer = setTimeout(() => void this.saveUrl(req, url), 400);
  }

  protected async sendFromUrl(): Promise<void> {
    clearTimeout(this.urlSaveTimer);
    const req = this.headerRequest();
    if (req) await this.saveUrl(req, this.urlDraft());
    await this.sendSelectedRequest(new Event('submit'));
  }

  protected readonly pawHint = computed(() =>
    this.state.view()?.kind === 'collection'
      ? 'You recently left your footprint here: you used the request the last time you entered this collection'
      : 'You recently left your footprint here: you used the request the last time you were here',
  );

  private async saveUrl(req: HttpRequestSummary, url: string): Promise<void> {
    if (url !== req.url) await this.updateRequestField(req, 'url', url);
  }

  readonly selectedRequests = computed<HttpRequestSummary[]>(() => {
    const ids = this.selectedRowIds();
    return this.activeRequests().filter((r) => ids.has(r.id));
  });

  /** What Delete does in the current view, shown in the context menu. */
  readonly deleteLabel = computed(() => {
    if (this.state.selectedFavouriteCollection()) return 'Remove from favourites';
    if (this.state.selectedScript()) return 'Detach from script';
    return 'Move to Trash';
  });

  readonly availableCollectionsForMove = computed<Collection[]>(() => {
    const current = this.state.selectedCollection();
    return this.collectionApi.collections().filter((c) => c.id !== current?.id);
  });

  protected readonly countLabel = computed(() => {
    const group = this.activeGroupName();
    if (!group) return null;
    const shown = this.filteredActiveRequests().length;
    const total = this.activeRequests().length;
    const searching = !!this.requestSearchQuery().trim() && shown !== total;
    return `${searching ? `${shown} of ${total}` : total} in ${group}`;
  });

  private readonly handleAddIntent = effect(() => {
    const intent = this.state.addRequestIntent();
    if (!intent) return;
    untracked(() => {
      this.state.addRequestIntent.set(null);
      const collection = this.state.selectedCollection();
      if (!collection) return;
      if (intent === 'blank') void this.addBlankRequest(collection);
      else this.curlImportFor.set(collection);
    });
  });

  constructor() {
    const clock = setInterval(() => this.now.set(Date.now()), 60_000);
    // Usage fades over days: load it per project, and again when the app
    // comes back to the front (a send reloads it too).
    const reloadUsage = () => {
      const projectId = this.state.selectedProject()?.id;
      if (projectId !== undefined && document.visibilityState === 'visible') {
        void this.requestApi.loadUsage(projectId);
      }
    };
    document.addEventListener('visibilitychange', reloadUsage);
    inject(DestroyRef).onDestroy(() => {
      clearInterval(clock);
      document.removeEventListener('visibilitychange', reloadUsage);
    });
    effect(() => {
      const projectId = this.state.selectedProject()?.id;
      if (projectId !== undefined) untracked(() => void this.requestApi.loadUsage(projectId));
    });

    effect(() => {
      const collection = this.state.selectedCollection();
      const favourite = this.state.selectedFavouriteCollection();
      const script = this.state.selectedScript();
      const showAll = this.state.showingAllRequests();
      const smart = this.state.smartView()?.kind ?? null;
      const showBin = this.state.showingBin();
      const project = this.state.selectedProject();

      // Remember the outgoing view's scroll position before it is replaced.
      const prevKey = this.lastGroupKey;
      if (prevKey !== null && this.viewport) {
        this.scrollOffsets.set(prevKey, this.viewport.measureScrollOffset('top'));
      }
      const nextKey = this.activeGroupKey();
      this.lastGroupKey = nextKey;

      const version = ++this.loadVersion;
      this.requestSearchQuery.set('');
      if (smart) {
        this.sortColumn.set(null);
        this.sortDirection.set('asc');
      }
      this.clearSelection();
      this.firstSelectPending.set(null);
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
          script,
          showAll,
          smart,
          showBin,
          project?.id ?? null,
          nextKey,
          true,
          nextKey !== null ? (this.scrollOffsets.get(nextKey) ?? null) : null,
        );
      });
    });

    effect(() => {
      this.state.multiSelectionActive.set(this.selectedRowIds().size > 1);
    });

    // Command-driven data refresh (undo/redo replays included): reload the
    // active group's data WITHOUT clearing selection or search, unlike the
    // view-switch effect above. The initial value 0 is skipped so app start
    // does not double-load.
    effect(() => {
      const refresh = this.changeBus.revision();
      if (refresh === 0) return;
      untracked(() => {
        const version = ++this.loadVersion;
        void this.loadActiveGroup(
          version,
          this.state.selectedCollection()?.id ?? null,
          this.state.selectedFavouriteCollection()?.id ?? null,
          this.state.selectedScript(),
          this.state.showingAllRequests(),
          this.state.smartView()?.kind ?? null,
          this.state.showingBin(),
          this.state.selectedProject()?.id ?? null,
          this.activeGroupKey(),
          false,
          null,
        );
      });
    });

    // Measured after rendering: the header cells' positions change with the
    // column widths, the window, and the action column (narrower without
    // the drag handle).
    afterRenderEffect(() => {
      this.methodColumnWidth();
      this.nameColumnWidth();
      this.resizeTick();
      this.canDragOut();
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
    script: string | null,
    showAll: boolean,
    smart: string | null,
    showBin: boolean,
    projectId: number | null,
    groupKeyOfView: string | null,
    scroll: boolean,
    restoreOffset: number | null,
  ): Promise<void> {
    try {
      if (showBin && projectId !== null) {
        await this.binApi.loadForProject(projectId);
        if (version !== this.loadVersion) return;
        this.restoreRememberedSelection(
          this.binSummaries(),
          groupKeyOfView,
          scroll,
          restoreOffset,
        );
        return;
      }

      // Smart views arrange the project's requests (see smartGroups).
      if (smart && projectId !== null) {
        const requests = this.activeRequests();
        const selectedId = this.restoreSelection(requests, groupKeyOfView);
        await this.scriptApi.loadScriptsForRequests(requests);
        if (version !== this.loadVersion) return;
        if (scroll) this.restoreScroll(selectedId, restoreOffset);
        return;
      }

      if (script) {
        const project = this.state.selectedProject();
        if (!project) return;
        const requests = await this.scriptApi.getRequestsForScript(project.id, script);
        if (version !== this.loadVersion) return;
        this.scriptRequests.set(requests);
        await this.scriptApi.loadScriptsForRequests(requests);
        if (version !== this.loadVersion) return;
        this.favouriteApi.requestsMembership.set({});
        this.restoreRememberedSelection(requests, groupKeyOfView, scroll, restoreOffset);
        return;
      }

      if (favouriteId !== null) {
        await this.favouriteApi.loadRequestsForCollection(favouriteId);
        if (version !== this.loadVersion) return;
        const requests = this.favouriteApi.requests();
        await this.scriptApi.loadScriptsForRequests(requests);
        if (version !== this.loadVersion) return;
        this.restoreRememberedSelection(requests, groupKeyOfView, scroll, restoreOffset);
        return;
      }

      if (collectionId !== null) {
        if (version !== this.loadVersion) return;
        const requests = this.requestApi.forCollection(collectionId);
        const selectedId = this.restoreSelection(requests, groupKeyOfView);
        await this.scriptApi.loadScriptsForRequests(requests);
        if (version !== this.loadVersion) return;
        if (scroll) this.restoreScroll(selectedId, restoreOffset);
        return;
      }

      if (showAll && projectId !== null) {
        if (version !== this.loadVersion) return;
        const requests = this.requestApi.forProject(projectId);
        const selectedId = this.restoreSelection(requests, groupKeyOfView);
        await this.scriptApi.loadScriptsForRequests(requests);
        if (version !== this.loadVersion) return;
        if (scroll) this.restoreScroll(selectedId, restoreOffset);
        return;
      }

      this.scriptRequests.set([]);
      this.favouriteApi.requestsMembership.set({});
    } catch (err) {
      console.error(err);
    }
  }

  private restoreRememberedSelection(
    requests: HttpRequestSummary[],
    groupKeyOfView: string | null,
    scroll: boolean,
    restoreOffset: number | null,
  ): void {
    const id = this.restoreSelection(requests, groupKeyOfView);
    if (scroll) this.restoreScroll(id, restoreOffset);
  }

  /** Selects the view's remembered rows, or its first request; returns the main row's id. */
  private restoreSelection(requests: HttpRequestSummary[], groupKeyOfView: string | null): number | null {
    const remembered =
      groupKeyOfView !== null ? this.selectionState.getSelection(groupKeyOfView) : null;

    // Keep only rows that are still in this list; the main row falls back to
    // the first remaining one.
    const present = new Set(requests.map((r) => r.id));
    const ids = remembered?.ids.filter((id) => present.has(id)) ?? [];
    if (!remembered || ids.length === 0) {
      this.selectFirstRequest(groupKeyOfView);
      return null;
    }
    const rememberedId = present.has(remembered.primary) ? remembered.primary : ids[0];
    const primary = requests.find((r) => r.id === rememberedId);
    if (!primary) return null;

    // Restore selection without letting the outer reactive effect track the
    // workspace request signal. Otherwise any later change to selectedRequest
    // (e.g. loading the full request for send) would re-run the group-load
    // effect, reset the selection, and scroll the list back to the remembered row.
    untracked(() => {
      this.selectCell(primary, 'name');
      if (ids.length > 1) {
        this.state.selectedIds.set(new Set(ids));
        this.rememberSelection(primary.id);
      }
    });
    return primary.id;
  }

  private restoreScroll(rememberedId: number | null, restoreOffset: number | null): void {
    if (rememberedId === null || !this.viewport) return;

    // Restore the exact scroll position this view had when we left it.
    if (restoreOffset !== null) {
      this.viewport.scrollToOffset(restoreOffset);
      return;
    }

    // First visit: centre the remembered row instead of pinning it to the top.
    const index = this.displayIndexById().get(rememberedId) ?? -1;
    if (index >= 0) {
      const viewportSize = this.viewport.getViewportSize();
      const offset = Math.max(0, index * this.rowHeight - (viewportSize - this.rowHeight) / 2);
      this.viewport.scrollToOffset(offset);
    }
  }

  // A selected request shows the URL bar above the list; selecting one up front
  // keeps the list from shifting down on the first click. A view whose requests
  // haven't arrived yet gets its first request when they do.
  private readonly firstSelectPending = signal<string | null>(null);

  private selectFirstRequest(groupKeyOfView: string | null): void {
    this.firstSelectPending.set(groupKeyOfView);
  }

  private readonly selectFirstWhenShown = effect(() => {
    const key = this.firstSelectPending();
    if (key === null) return;
    const first = this.displayRows().find((row) => row.kind === 'request')?.req;
    if (!first) return;
    untracked(() => {
      this.firstSelectPending.set(null);
      if (key === this.activeGroupKey() && this.selectedRowIds().size === 0) {
        this.selectCell(first as HttpRequestSummary, 'name', false);
      }
    });
  });

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
      return;
    }

    if (event.key === 'Delete') {
      if (isEditableTarget(event.target) || this.isBusyDialogOpen()) return;
      if (this.selectedRowIds().size === 0) return;
      event.preventDefault();
      if (this.state.showingBin()) {
        void this.requestPermanentDelete();
      } else {
        void this.deleteSelectedRequests();
      }
    }
  }

  /** True while any dialog/overlay is open, so global Delete does not act behind it. */
  private isBusyDialogOpen(): boolean {
    return (
      this.confirm.active() !== null ||
      this.contextMenu.isOpen() ||
      this.editingNameId() !== null ||
      this.editModalOpen() ||
      this.scriptPickerFor() !== null ||
      this.favouritePickerFor() !== null ||
      this.moveCollectionModalOpen() ||
      this.restorePickerFor() !== null ||
      this.curlImportFor() !== null
    );
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
    if (this.moveCollectionModalOpen()) {
      this.closeMoveCollectionModal();
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

    this.rememberSelection(req.id);
  }

  /**
   * Remembers the whole selection for the list being viewed, so it comes back
   * when the user leaves the list and returns.
   */
  private rememberSelection(primaryId: number): void {
    const key = this.activeGroupKey();
    if (key === null) return;
    const ids = this.selectedRowIds();
    if (ids.size === 0) {
      this.selectionState.clearSelection(key);
      return;
    }
    this.selectionState.setSelection(key, ids.has(primaryId) ? primaryId : [...ids][0], ids);
  }

  protected clearSelection(): void {
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
    this.rememberSelection(req.id);
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

    void this.requestApi.getFull(id).then(full => {
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
    this.rememberSelection(req.id);
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
    const rows = this.displayRows();
    const rowIndex = Math.max(
      0,
      Math.min(Math.floor((scrollOffset + relativeY) / rowHeight), rows.length - 1),
    );
    // A smart view's heading row stands for the first request below it.
    const row = rows[rowIndex]?.kind === 'heading' ? rows[rowIndex + 1] : rows[rowIndex];
    if (row?.kind !== 'request') return;

    const requests = this.filteredActiveRequests();
    const currentReq = row.req;
    const index = requests.findIndex((r) => r.id === currentReq.id);
    if (index === -1) return;

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
      const consumedBySidebar = drag ? this.dragSession.tryDrop() : false;
      this.reorderPending.set(null);
      this.reorderDrag.set(null);
      this.stopReorderFrameLoop();
      document.body.classList.remove('reorder-dragging');
      this.dragSession.end();
      if (drag) {
        this.ignoreNextClick.set(true);
        if (!consumedBySidebar && drag.fromIndex >= 0 && this.canReorderRows()) {
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
    if (this.selectedRowIds().size > 1 && this.selectedRowIds().has(req.id)) return;

    this.dragSession.start({ requestIds: [req.id], request: req });
    this.reorderPending.set({
      requestId: req.id,
      startY: event.clientY,
      externalOnly: !this.canReorderRows(),
      request: req,
    });
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
      this.reorderPending.set(null);

      if (pending.externalOnly) {
        this.reorderLastX = event.clientX;
        this.reorderLastY = event.clientY;
        this.reorderInsertIndex = 0;
        this.reorderDrag.set({ request: pending.request, fromIndex: -1 });
        this.startReorderFrameLoop();
        document.body.classList.add('reorder-dragging');
        return;
      }

      const requests = this.filteredActiveRequests();
      const fromIndex = requests.findIndex(r => r.id === pending.requestId);
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
    const overSidebar =
      drag.fromIndex < 0 ||
      this.dragSession.hovered() !== null ||
      this.dragSession.overEmptyCollections();

    if (!overSidebar) {
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
      line.style.top = `${lineTop}px`;
      line.style.left = `${rect.left + 4}px`;
      line.style.width = `${Math.max(0, rect.width - 8)}px`;
      line.style.visibility = 'visible';
    } else {
      line.style.visibility = 'hidden';
    }

    ghost.style.left = `${this.reorderLastX}px`;
    ghost.style.top = `${this.reorderLastY}px`;
    // Hidden by CSS until first positioned, so the ghost never flashes at
    // its unpositioned coordinates.
    ghost.style.visibility = 'visible';
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
      const next = [...this.requestApi.forCollection(collection.id)];
      const [moved] = next.splice(from, 1);
      if (!moved) return;
      next.splice(to, 0, moved);
      const orders: Record<number, number> = {};
      for (const [index, req] of next.entries()) {
        orders[req.id] = index;
      }
      this.requestApi.applyUserSortOrders(orders);
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

    if (kind === 'script') {
      const next = [...this.scriptRequests()];
      const [moved] = next.splice(from, 1);
      if (!moved) return;
      next.splice(to, 0, moved);
      this.scriptRequests.set(next.map((req, index) => ({ ...req, user_sort_order: index })));
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
      } else if (kind === 'script') {
        const project = this.state.selectedProject();
        const script = this.state.selectedScript();
        if (!project || !script) return;
        await this.scriptApi.updateRequestOrder(project.id, script, orderedIds);
      }
    } catch (err) {
      console.error('persisting request order failed, reloading view', err);
      try {
        if (kind === 'collection') {
          await this.requestApi.loadAll();
        } else if (kind === 'favourite') {
          const favourite = this.state.selectedFavouriteCollection();
          if (favourite) await this.favouriteApi.loadRequestsForCollection(favourite.id);
        } else if (kind === 'script') {
          const project = this.state.selectedProject();
          const script = this.state.selectedScript();
          if (project && script) {
            this.scriptRequests.set(await this.scriptApi.getRequestsForScript(project.id, script));
          }
        }
      } catch (reloadErr) {
        console.error('reloading view after failed reorder', reloadErr);
      }
    }
  }

  /**
   * Applies a colour (or clears with null) to all selected requests in the
   * current view. Optimistic single-write update, then a scoped bulk API;
   * on failure the view is reloaded.
   */
  async applyRequestColor(color: string | null): Promise<void> {
    const ids = [...this.selectedRowIds()];
    if (ids.length === 0) return;

    const kind = this.activeViewKind();
    try {
      if (kind === 'collection') {
        const colors: Record<number, string | null> = {};
        for (const id of ids) colors[id] = color;
        this.requestApi.applyUserColors(colors);
        await this.requestApi.bulkUpdateColor(ids, color);
      } else if (kind === 'favourite') {
        const favourite = this.state.selectedFavouriteCollection();
        if (!favourite) return;
        const idSet = new Set(ids);
        this.favouriteApi.requests.set(
          this.favouriteApi.requests().map(r => (idSet.has(r.id) ? { ...r, user_color: color } : r)),
        );
        await this.favouriteApi.updateRequestColor(favourite.id, ids, color);
      } else if (kind === 'script') {
        const project = this.state.selectedProject();
        const script = this.state.selectedScript();
        if (!project || !script) return;
        const idSet = new Set(ids);
        this.scriptRequests.set(
          this.scriptRequests().map(r => (idSet.has(r.id) ? { ...r, user_color: color } : r)),
        );
        await this.scriptApi.updateRequestColor(project.id, script, ids, color);
      }
    } catch (err) {
      console.error('persisting request colour failed, reloading view', err);
      try {
        if (kind === 'collection') {
          await this.requestApi.loadAll();
        } else if (kind === 'favourite') {
          const favourite = this.state.selectedFavouriteCollection();
          if (favourite) await this.favouriteApi.loadRequestsForCollection(favourite.id);
        } else if (kind === 'script') {
          const project = this.state.selectedProject();
          const script = this.state.selectedScript();
          if (project && script) {
            this.scriptRequests.set(await this.scriptApi.getRequestsForScript(project.id, script));
          }
        }
      } catch (reloadErr) {
        console.error('reloading view after failed colour update', reloadErr);
      }
    }
  }

  /**
   * Clears the colour on every request in the current view in one bulk call.
   */
  async clearAllRequestColors(): Promise<void> {
    if (!this.state.view()?.canColour) return;
    const kind = this.activeViewKind();

    const ids = this.activeRequests().map(r => r.id);
    if (ids.length === 0) return;

    try {
      if (kind === 'collection') {
        const colors: Record<number, string | null> = {};
        for (const id of ids) colors[id] = null;
        this.requestApi.applyUserColors(colors);
        await this.requestApi.bulkUpdateColor(ids, null);
      } else if (kind === 'favourite') {
        const favourite = this.state.selectedFavouriteCollection();
        if (!favourite) return;
        this.favouriteApi.requests.set(this.favouriteApi.requests().map(r => ({ ...r, user_color: null })));
        await this.favouriteApi.updateRequestColor(favourite.id, ids, null);
      } else if (kind === 'script') {
        const project = this.state.selectedProject();
        const script = this.state.selectedScript();
        if (!project || !script) return;
        this.scriptRequests.set(this.scriptRequests().map(r => ({ ...r, user_color: null })));
        await this.scriptApi.updateRequestColor(project.id, script, ids, null);
      }
    } catch (err) {
      console.error('clearing all request colours failed, reloading view', err);
      try {
        if (kind === 'collection') {
          await this.requestApi.loadAll();
        } else if (kind === 'favourite') {
          const favourite = this.state.selectedFavouriteCollection();
          if (favourite) await this.favouriteApi.loadRequestsForCollection(favourite.id);
        } else if (kind === 'script') {
          const project = this.state.selectedProject();
          const script = this.state.selectedScript();
          if (project && script) {
            this.scriptRequests.set(await this.scriptApi.getRequestsForScript(project.id, script));
          }
        }
      } catch (reloadErr) {
        console.error('reloading view after failed colour clear', reloadErr);
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
      const full = await this.requestApi.getFull(req.id);
      // Secrets are left out: the backend fills them in from the environment.
      const plain = this.state.environmentVariables().filter((v) => !v.secret);
      const curl = await this.requestApi.requestToCurl(full, plain, this.state.selectedEnvironment()?.id ?? null);
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
        duplicated.push({ originalId: original.id, request: await this.requestApi.duplicate(original.id) });
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
        this.scriptApi.loadScriptsForRequests(duplicatedRequests),
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

  /** Asks for confirmation, then permanently deletes the selected bin items. */
  async requestPermanentDelete(): Promise<void> {
    const ids = [...this.selectedRowIds()];
    if (ids.length === 0) return;

    const confirmed = await this.confirm.ask({
      title: 'Delete permanently?',
      message:
        ids.length === 1
          ? 'This request will be permanently deleted. This cannot be undone.'
          : `These ${ids.length} requests will be permanently deleted. This cannot be undone.`,
      confirmLabel: 'Delete permanently',
      danger: true,
    });
    if (!confirmed) return;

    await this.deleteSelectedRequests();
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
      const script = this.state.selectedScript();

      if (favourite) {
        for (const id of ids) {
          await this.favouriteApi.removeRequest(favourite.id, id);
        }
        this.favouriteApi.requests.update(list => list.filter(r => !ids.includes(r.id)));
      } else if (script) {
        // Inside a script view, delete means "detach from this script": the
        // requests stay in their collections and nothing goes to the Trash.
        for (const id of ids) {
          await this.scriptApi.detachScript(id, script);
        }
      } else {
        await this.requestApi.deleteMany(ids);
        for (const id of ids) {
          this.selectionState.deleteRequest(id);
        }
      }

      if (this.state.selectedRequest() && ids.includes(this.state.selectedRequest()!.id)) {
        this.state.selectedRequest.set(null);
        this.state.selectedResponse.set(null);
      }

      this.clearSelection();

      if (script) {
        this.scriptRequests.update(list => list.filter(r => !ids.includes(r.id)));
        await this.scriptApi.loadScriptsForRequests(this.scriptRequests());
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
    void this.restoreOrPick(selectedIds);
  }

  private async restoreOrPick(binIds: number[]): Promise<void> {
    this.state.loading.set(true);
    try {
      if (await this.binApi.restoreToOriginal(binIds)) {
        this.state.selectedBinnedRequest.set(null);
        this.clearSelection();
      } else {
        this.restorePickerFor.set(binIds);
      }
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
        await this.requestApi.move(req.id, collection.id, collection.project_id);
      }

      this.clearSelection();
      this.closeMoveCollectionModal();
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }

  // ---------- Script picker ----------

  /** Opens the script picker for one request or the current selection. */
  openScriptPicker(requests: HttpRequestSummary[]): void {
    this.contextMenu.close();
    if (requests.length > 0) this.scriptPickerFor.set(requests);
  }

  // ---------- Favourite picker ----------

  /** Opens the favourite picker for one request or the current selection. */
  openFavouritePicker(requests: HttpRequestSummary[]): void {
    this.contextMenu.close();
    if (requests.length > 0) this.favouritePickerFor.set(requests);
  }

  // ---------- Sorting ----------

  /**
   * Column sort cycles asc -> desc -> none. The third click clears the sort
   * so the list returns to the user's custom order (or the view's natural
   * order on non-orderable views).
   */
  toggleSort(column: ColumnKey): void {
    if (!this.sortable()) return;
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
      case 'scripts':
        return (this.requestScripts()[req.id] ?? []).join(', ');
      default:
        return '';
    }
  }

  // ---------- New request ----------

  /** "+ Request": creates a blank request in the collection and starts renaming it. */
  async addBlankRequest(collection: Collection): Promise<void> {
    this.contextMenu.close();
    this.state.loading.set(true);
    try {
      this.revealAndRename(await this.requestApi.createBlank(collection));
    } catch (err) {
      console.error('creating request failed', err);
    } finally {
      this.state.loading.set(false);
    }
  }

  /** Selects a just-created request, scrolls to it and opens its name for editing. */
  revealAndRename(req: HttpRequestSummary): void {
    // A search could hide the new row; clear it so the rename field can show.
    this.requestSearchQuery.set('');
    this.selectCell(req, 'name');
    this.startInlineNameEdit(req);
    // Scroll once the list has drawn the new row, so the offset isn't clamped
    // to the old list height; the rename field focuses itself when it appears.
    afterNextRender(
      () => {
        const index = this.displayIndexById().get(req.id) ?? -1;
        if (index < 0 || !this.viewport) return;
        this.viewport.checkViewportSize();
        const viewportSize = this.viewport.getViewportSize();
        this.viewport.scrollToOffset(Math.max(0, index * this.rowHeight - (viewportSize - this.rowHeight) / 2));
      },
      { injector: this.injector },
    );
  }

  async sendSelectedRequest(event: Event): Promise<void> {
    if (this.state.showingBin()) return;

    let req = this.state.selectedRequest();
    if (!req) {
      const selectedIds = [...this.selectedRowIds()];
      const selectedId = this.selectedCell()?.requestId ?? selectedIds[0];
      if (!selectedId) return;
      try {
        req = await this.requestApi.getFull(selectedId);
      } catch (err) {
        console.error(err);
        return;
      }
      this.state.selectedRequest.set(req);
    }
    await this.state.sendRequest(req, event);
  }

  onFavouriteButtonClick(): void {
    if (this.selectedRowIds().size <= 1) {
      const req = this.selectedRequest() ?? this.selectedRequests()[0];
      if (req) this.openFavouritePicker([req]);
    } else {
      this.openFavouritePicker(this.selectedRequests());
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
    if (column === 'scripts') {
      this.openScriptPicker([request]);
      return;
    }
    this.openEditModal(request, column);
  }

  // ---------- Inline name edit ----------

  startInlineNameEdit(req: HttpRequestSummary): void {
    this.contextMenu.close();
    this.editingNameId.set(req.id);
    this.editingNameValue.set(req.name);
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

    if (column === 'scripts') return;

    const field = this.columnToRequestField(column);
    await this.updateRequestField(req, field, this.editModalValue());
    this.closeEditModal();
  }

  protected async chooseMethod(req: HttpRequestSummary, method: string): Promise<void> {
    if (method === req.method) return;
    await this.updateRequestField(req, 'method', method);
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
      const full = await this.requestApi.getFull(req.id);
      const updated = { ...full, [field]: value } as HttpRequest;
      const saved = await this.requestApi.update(updated);
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
    this.requestApi.patch(id, changes);

    const patch = (list: HttpRequestSummary[]) => {
      const index = list.findIndex((r) => r.id === id);
      if (index === -1) return list;
      const next = [...list];
      next[index] = { ...next[index], ...changes };
      return next;
    };

    this.favouriteApi.requests.update(patch);
    this.scriptRequests.update(patch);
  }
}
