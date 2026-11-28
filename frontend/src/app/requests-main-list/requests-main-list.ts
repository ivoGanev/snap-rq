import { ChangeDetectionStrategy, Component, DestroyRef, ElementRef, Injector, OnDestroy, TemplateRef, ViewChild, afterNextRender, afterRenderEffect, computed, effect, inject, linkedSignal, signal, untracked, viewChild } from '@angular/core';
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
import { REQUEST_COLORS, requestColorInfo } from '../core/request-colors';
import type { WorkspaceViewKind } from '../core/workspace/workspace-view';
import type { RequestGroup } from '../core/workspace/presorted';
import { toEntries, UNGROUPED, type GroupEntry, type ListEntry, type ListSection } from '../core/workspace/list-entries';
import { GroupApiService, type RequestGroupRecord } from '../core/services/group.service';
import {
  arrangeCollection,
  flattenLayout,
  groupDropTarget,
  moveGroup,
  moveRequests,
  requestDropTarget,
  sameLayout,
  type DropTarget,
  type LayoutItem,
} from '../core/workspace/collection-layout';

type ColumnKey = 'name' | 'url' | 'method' | 'scripts';
type SortDirection = 'asc' | 'desc';

type DisplayRow = ListEntry;

interface SelectedCell {
  requestId: number;
  column: ColumnKey;
}

interface RowState {
  isSelected: boolean;
  isCellSelected: Record<ColumnKey, boolean>;
  /** URL with environment variables resolved, for display only. */
  displayUrl: string;
  /** A favourite's or script's own colour for this request (the left bar), or null. */
  colorVar: string | null;
  /** Row background tint matching colorVar, or null. */
  colorTint: string | null;
  /** The colour of the collection group it is in (the dashed outline), or null. */
  groupColor: string | null;
}

interface ReorderDrag {
  /** Snapshot of the dragged row shown in the ghost. */
  request: HttpRequestSummary;
  fromIndex: number;
  /** Every request being dragged: the selection when a selected row's handle was grabbed. */
  ids: number[];
  /** Dropping on the list moves the requests to the line's place in the collection's layout. */
  arrange: boolean;
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
// Right of this (from the list's left edge) a drop at a group's last row stays inside the group.
const NEST_POINTER_PX = 120;
const NEST_LINE_INDENT_PX = 36;
const NAME_COLUMN_MIN_WIDTH = 120;

/**
 * Spreadsheet-style request list with multi-select and bulk actions.
 */
@Component({
  selector: 'app-requests-main-list',
  imports: [CdkVirtualScrollViewport, CdkVirtualForOf, CdkFixedSizeVirtualScroll, RequestBottomDrawer, CodeEditor, Select, ScriptTabs, ScriptPickerDialog, FavouritePickerDialog, RestoreToCollectionDialog, CurlImportDialog, SendButton, UsageMeter, LastUsedPaw],
  templateUrl: './requests-main-list.html',
  styleUrl: './requests-main-list.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: {
    class: 'main-column',
    'aria-label': 'Requests',
    '(window:keydown)': 'onWindowKeydown($event)',
    '(window:mousemove)': 'onWindowMouseMove($event)',
    '(window:mouseup)': 'onWindowMouseUp($event)',
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
  private readonly groupApi = inject(GroupApiService);

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

  /** The group picked by clicking its heading (collections only); the colour picker colours it. */
  readonly selectedGroupId = this.state.selectedGroupId;

  protected readonly selectedGroup = computed<RequestGroupRecord | null>(() => {
    const id = this.selectedGroupId();
    return id === null ? null : (this.groupApi.groups().find((g) => g.id === id) ?? null);
  });

  /** The open collection's groups, in the user's order. */
  protected readonly collectionGroups = computed<RequestGroupRecord[]>(() => {
    const collection = this.state.selectedCollection();
    if (!collection) return [];
    return this.groupApi
      .groups()
      .filter((g) => g.collection_id === collection.id)
      .sort((a, b) => a.sort_order - b.sort_order || a.name.localeCompare(b.name));
  });

  /** The bottom drawer's colour picker: favourites and scripts colour single requests; a group is coloured from the URL bar. */
  readonly canColorSelection = computed(() => {
    if (this.activeViewKind() === 'collection') return false;
    return (this.state.view()?.canColour ?? false) && this.selectedRowIds().size > 0;
  });

  protected readonly colors = REQUEST_COLORS;

  /** The colour shared by all selected rows in this view, or null when mixed/none. */
  readonly activeColor = computed<string | null>(() => {
    if (this.activeViewKind() === 'collection') return this.selectedGroup()?.color ?? null;
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
    ids: number[];
    arrange: boolean;
  } | null>(null);

  /** Where a drag in a collection would land; read on release. */
  private dropTarget: DropTarget | null = null;
  /** The group heading a request drag would drop into. */
  readonly dropIntoGroupId = signal<number | null>(null);
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

  private readonly requestNameField = viewChild<ElementRef<HTMLInputElement>>('requestNameField');
  private readonly groupNameField = viewChild<ElementRef<HTMLInputElement>>('groupNameField');
  /** A just-created request or group whose name the toolbar should put the cursor in. */
  private readonly nameToFocus = signal<{ kind: 'request' | 'group'; id: number } | null>(null);
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

  /**
   * The list in sections under headings: a smart view's buckets, or a
   * collection's groups (by name) and then its ungrouped requests. Null for
   * the flat views. Searching keeps only matching requests and sections.
   */
  readonly sections = computed<ListSection[] | null>(() => {
    const words = this.searchWords();
    const matching = words.length
      ? new Set(filterRequests(this.searchIndex(), words).map((r) => r.id))
      : null;
    const keep = (reqs: HttpRequestSummary[]) => (matching ? reqs.filter((r) => matching.has(r.id)) : reqs);

    const smart = this.smartGroups();
    if (smart) {
      return smart
        .map((g): ListSection => ({
          heading: { key: `smart:${g.key}`, label: g.label, color: null, groupId: null },
          requests: keep(g.requests),
        }))
        .filter((section) => section.requests.length > 0);
    }

    const collection = this.state.selectedCollection();
    if (!collection) return null;

    const column = this.sortColumn();
    const top = arrangeCollection(this.collectionGroups(), this.requestApi.forCollection(collection.id));
    const heading = (g: RequestGroupRecord) => ({
      key: `group:${g.id}`,
      label: g.name,
      color: requestColorInfo(g.color)?.cssVar ?? null,
      groupId: g.id,
    });

    if (column) {
      // A column sort orders the requests inside each group; groups keep their order, after the ungrouped requests.
      const compare = (a: HttpRequestSummary, b: HttpRequestSummary) =>
        this.compareRequests(a, b, column) || a.name.localeCompare(b.name);
      const loose = [...keep(top.flatMap((it) => (it.kind === 'request' ? [it.req] : [])))].sort(compare);
      const sorted: ListSection[] = loose.length ? [{ heading: null, requests: loose }] : [];
      for (const it of top) {
        if (it.kind !== 'group') continue;
        const reqs = [...keep(it.members)].sort(compare);
        if (!matching || reqs.length) sorted.push({ heading: heading(it.group), requests: reqs });
      }
      return sorted;
    }

    const sections: ListSection[] = [];
    for (const it of top) {
      if (it.kind === 'request') {
        if (matching && !matching.has(it.req.id)) continue;
        const last = sections[sections.length - 1];
        if (last && last.heading === null) last.requests.push(it.req);
        else sections.push({ heading: null, requests: [it.req] });
        continue;
      }
      const reqs = keep(it.members);
      if (matching && reqs.length === 0) continue;
      sections.push({ heading: heading(it.group), requests: reqs });
    }
    return sections;
  });

  /** Drags in a collection place items where they are dropped, which needs the user's order on screen. */
  protected readonly canArrange = computed(
    () => this.activeViewKind() === 'collection' && this.sortColumn() === null && this.searchWords().length === 0,
  );

  private collectionLayout(): LayoutItem[] {
    const collection = this.state.selectedCollection();
    if (!collection) return [];
    return flattenLayout(arrangeCollection(this.collectionGroups(), this.requestApi.forCollection(collection.id)));
  }

  private applyLayout(next: LayoutItem[]): void {
    const collection = this.state.selectedCollection();
    if (!collection || sameLayout(next, this.collectionLayout())) return;
    const changes = new Map<number, Partial<HttpRequestSummary>>();
    next.forEach((it, index) => {
      if (it.kind !== 'request') return;
      const current = this.requestApi.get(it.id);
      if (current?.user_sort_order !== index || (current.group_id ?? null) !== it.groupId) {
        changes.set(it.id, { user_sort_order: index, group_id: it.groupId });
      }
    });
    this.requestApi.patchMany(changes);
    this.groupApi.arrange(collection.id, next).catch((err) => console.error('moving requests failed', err));
  }

  private placeDropLine(line: HTMLElement, rect: DOMRect, row: number, offset: number, indent: boolean): void {
    const inset = indent ? NEST_LINE_INDENT_PX : 0;
    line.style.top = `${rect.top + row * this.rowHeight - offset}px`;
    line.style.left = `${rect.left + 4 + inset}px`;
    line.style.width = `${Math.max(0, rect.width - 8 - inset)}px`;
    line.style.visibility = 'visible';
  }

  /** Headings folded by the user, saved across launches as `<list>|<heading>` keys. */
  private readonly foldedKeys = signal<Set<string>>(loadFoldedKeys());

  protected isCollapsed(key: string): boolean {
    if (this.searchWords().length) return false;
    return this.foldedKeys().has(`${this.activeGroupKey()}|${key}`);
  }

  toggleFold(entry: GroupEntry, event?: Event): void {
    event?.stopPropagation();
    const key = `${this.activeGroupKey()}|${entry.key}`;
    this.foldedKeys.update((keys) => {
      const next = new Set(keys);
      if (!next.delete(key)) next.add(key);
      saveFoldedKeys(next);
      return next;
    });
  }

  /**
   * The rows shown. Searching ranks them (best match, then recent usage), unless
   * a column sort is chosen: then it only filters and the column decides.
   */
  readonly filteredActiveRequests = computed<HttpRequestSummary[]>(() => {
    const sections = this.sections();
    if (sections) {
      return sections.flatMap((section) =>
        section.heading && this.isCollapsed(section.heading.key) ? [] : section.requests,
      );
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

  /** What the list draws: request rows, plus the headings of a sectioned list. */
  readonly displayRows = computed<DisplayRow[]>(() => {
    const sections = this.sections();
    if (!sections) {
      return this.filteredActiveRequests().map(
        (req, i): DisplayRow => ({ kind: 'request', req, depth: 0, section: UNGROUPED, even: i % 2 === 1, last: false }),
      );
    }
    return toEntries(sections, (key) => this.isCollapsed(key));
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
    return row.kind === 'group' ? `group:${row.key}` : row.req.id;
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
    const groupColors = new Map(this.groupApi.groups().map((g) => [g.id, g.color]));
    // Favourites and scripts colour single requests; a collection outlines its coloured groups;
    // other views show no colour.
    const ownColors = this.activeViewKind() === 'favourite' || this.activeViewKind() === 'script';
    const inCollection = this.activeViewKind() === 'collection';

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
      const own = ownColors ? requestColorInfo(req.user_color) : null;
      const group = inCollection ? requestColorInfo(groupColors.get(req.group_id ?? -1) ?? null) : null;
      result[req.id] = {
        isSelected,
        isCellSelected,
        displayUrl: environment.interpolate(req.url),
        colorVar: own?.cssVar ?? null,
        colorTint: own?.tint ?? null,
        groupColor: group?.cssVar ?? null,
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
  // Always shown (but in the bin), so the list doesn't shift as the selection changes.
  protected readonly showHeader = computed(() => !this.state.showingBin() && this.activeGroupName() !== null);
  // Reset only when another request is selected: the saved URL coming back
  // mid-typing would otherwise overwrite what was typed since.
  protected readonly urlDraft = linkedSignal<number | null, string>({
    source: () => this.headerRequest()?.id ?? null,
    computation: () => untracked(() => this.headerRequest()?.url ?? ''),
  });

  protected onUrlChange(url: string): void {
    this.urlDraft.set(url);
    const req = this.headerRequest();
    if (req) void this.saveUrl(req, url);
  }

  protected async sendFromUrl(): Promise<void> {
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
  /** The toolbar's label for deleting; the tooltip carries deleteLabel. */
  protected readonly deleteShortLabel = computed(() => {
    if (this.state.selectedFavouriteCollection()) return 'Remove';
    if (this.state.selectedScript()) return 'Detach';
    return 'Trash';
  });

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
        const selectedId = this.restoreSelection(requests, groupKeyOfView, scroll);
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
        const selectedId = this.restoreSelection(requests, groupKeyOfView, scroll);
        await this.scriptApi.loadScriptsForRequests(requests);
        if (version !== this.loadVersion) return;
        if (scroll) this.restoreScroll(selectedId, restoreOffset);
        return;
      }

      if (showAll && projectId !== null) {
        if (version !== this.loadVersion) return;
        const requests = this.requestApi.forProject(projectId);
        const selectedId = this.restoreSelection(requests, groupKeyOfView, scroll);
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
    const id = this.restoreSelection(requests, groupKeyOfView, scroll);
    if (scroll) this.restoreScroll(id, restoreOffset);
  }

  /**
   * Selects the view's remembered rows; returns the main row's id. With none
   * remembered, a view being opened selects its first request, while a
   * refresh after a command keeps an empty selection (a picked group, say).
   */
  private restoreSelection(
    requests: HttpRequestSummary[],
    groupKeyOfView: string | null,
    opening: boolean,
  ): number | null {
    const remembered =
      groupKeyOfView !== null ? this.selectionState.getSelection(groupKeyOfView) : null;

    // Keep only rows that are still in this list; the main row falls back to
    // the first remaining one.
    const present = new Set(requests.map((r) => r.id));
    const ids = remembered?.ids.filter((id) => present.has(id)) ?? [];
    if (!remembered || ids.length === 0) {
      if (opening) this.selectFirstRequest(groupKeyOfView);
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
      if (this.cancelGroupDrag() || this.cancelReorder()) return;
      this.onEscapePressed();
      return;
    }

    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'd') {
      event.preventDefault();
      if (this.state.showingBin()) return;
      void this.duplicateSelectedRequest();
      return;
    }

    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'g') {
      if (isEditableTarget(event.target) || this.activeViewKind() !== 'collection') return;
      event.preventDefault();
      void this.groupSelection();
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
      this.scriptPickerFor() !== null ||
      this.favouritePickerFor() !== null ||
      this.moveCollectionModalOpen() ||
      this.restorePickerFor() !== null ||
      this.curlImportFor() !== null
    );
  }

  onEscapePressed(): void {
    if (this.contextMenu.isOpen()) {
      this.contextMenu.close();
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

  private readonly dropPickedGroup = effect(() => {
    if (this.selectedRowIds().size > 0) untracked(() => this.selectedGroupId.set(null));
  });

  private readonly dropPickedGroupOnViewChange = effect(() => {
    this.state.view();
    untracked(() => this.selectedGroupId.set(null));
  });

  protected clearSelection(): void {
    this.state.selectedIds.set(new Set());
    this.selectedCell.set(null);
    this.highlightedCell.set(null);
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

  // ---------- Row drag ----------

  /**
   * Pressing on a row arms a drag; it starts once the pointer moves past the
   * threshold, so a press and release without moving stays a click.
   */
  onRowMouseDown(req: HttpRequestSummary | HttpBinnedRequestSummary, event: MouseEvent): void {
    if (event.button !== 0) return;
    // A new mousedown always starts a new gesture: clear any stale
    // ignoreNextClick left over from a previous drag release whose trailing
    // click never landed on a row. Without this, the first deliberate click
    // after a drag would be swallowed (requiring two clicks to select).
    this.ignoreNextClick.set(false);
    if (event.ctrlKey || event.metaKey || event.shiftKey) return;
    if (!this.canDragOut() || isEditableTarget(event.target)) return;
    // Stops the press from starting a text selection across rows.
    event.preventDefault();

    const request = req as HttpRequestSummary;
    const selected = this.selectedRowIds();
    const arrange = this.canArrange();
    // Outside collections only the pressed row moves: reordering several at once is ambiguous.
    const many = this.activeViewKind() === 'collection' && selected.size > 1 && selected.has(req.id);
    const ids = many ? [...selected] : [req.id];
    this.reorderPending.set({
      requestId: req.id,
      startY: event.clientY,
      externalOnly: !this.canReorderRows(),
      request,
      ids,
      arrange,
    });
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

    if (this.groupDragPending) {
      this.handleGroupDragMove(event);
      return;
    }

    if (this.reorderPending() !== null || this.reorderDrag() !== null) {
      this.handleReorderMouseMove(event);
      return;
    }

  }

  onWindowMouseUp(event?: MouseEvent): void {
    // Drop targets are worked out per animation frame; a quick release can
    // come before the frame that saw the pointer's last move, so one more
    // frame runs here at the release point.
    if (event) {
      this.reorderLastX = event.clientX;
      this.reorderLastY = event.clientY;
    }

    if (this.groupDragPending) {
      if (this.groupDrag()) this.renderGroupDragFrame();
      this.finishGroupDrag();
      return;
    }

    if (this.reorderPending() !== null || this.reorderDrag() !== null) {
      if (this.reorderDrag()) this.renderReorderFrame();
      const drag = this.reorderDrag();
      const consumedBySidebar = drag ? this.dragSession.tryDrop() : false;
      this.reorderPending.set(null);
      this.reorderDrag.set(null);
      this.stopReorderFrameLoop();
      document.body.classList.remove('reorder-dragging');
      this.dragSession.end();
      const target = this.dropTarget;
      this.dropTarget = null;
      this.dropIntoGroupId.set(null);
      if (drag) {
        this.ignoreNextClick.set(true);
        if (drag.arrange) {
          if (!consumedBySidebar && target) {
            this.applyLayout(moveRequests(this.collectionLayout(), new Set(drag.ids), target));
          }
          return;
        }
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

  }

  // ---------- Row reorder drag ----------

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
      this.dragSession.start({ requestIds: pending.ids, request: pending.request });
      if (!this.selectedRowIds().has(pending.requestId)) {
        this.selectCell(pending.request, 'name', false);
        this.lastClickedRowId.set(pending.requestId);
      }

      if (pending.externalOnly) {
        this.reorderLastX = event.clientX;
        this.reorderLastY = event.clientY;
        this.reorderInsertIndex = 0;
        this.reorderDrag.set({
          request: pending.request,
          fromIndex: -1,
          ids: pending.ids,
          arrange: pending.arrange,
        });
        this.startReorderFrameLoop();
        document.body.classList.add('reorder-dragging');
        return;
      }

      const requests = this.filteredActiveRequests();
      const fromIndex = requests.findIndex(r => r.id === pending.requestId);
      if (fromIndex === -1 || !this.canReorderRows()) {
        this.dragSession.end();
        return;
      }
      this.reorderLastX = event.clientX;
      this.reorderLastY = event.clientY;
      this.reorderInsertIndex = fromIndex;
      this.reorderDrag.set({ request: requests[fromIndex], fromIndex, ids: [pending.requestId], arrange: false });
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
      (drag.fromIndex < 0 && !drag.arrange) ||
      this.dragSession.hovered() !== null ||
      this.dragSession.overEmptyCollections();

    if (drag.arrange) {
      const over =
        !overSidebar &&
        this.reorderLastX >= rect.left &&
        this.reorderLastX <= rect.right &&
        this.reorderLastY >= rect.top &&
        this.reorderLastY <= rect.bottom;
      line.style.visibility = 'hidden';
      this.dropTarget = null;
      if (over) {
        this.autoScrollReorder(rect);
        const offset = this.viewport.measureScrollOffset('top');
        const target = requestDropTarget(
          this.displayRows(),
          offset + this.reorderLastY - rect.top,
          this.rowHeight,
          this.reorderLastX > rect.left + NEST_POINTER_PX,
        );
        const layout = this.collectionLayout();
        if (sameLayout(moveRequests(layout, new Set(drag.ids), target), layout)) {
          // Dropping here changes nothing, so no line.
          this.dropTarget = null;
        } else {
          this.dropTarget = target;
          if (target.lineRow !== null) this.placeDropLine(line, rect, target.lineRow, offset, target.indent);
        }
      }
      const into = this.dropTarget?.into ?? null;
      if (into !== this.dropIntoGroupId()) this.dropIntoGroupId.set(into);
      ghost.style.left = `${this.reorderLastX}px`;
      ghost.style.top = `${this.reorderLastY}px`;
      ghost.style.visibility = 'visible';
      return;
    }

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

  private autoScrollReorder(rect: DOMRect): void {
    if (this.reorderLastY < rect.top + REORDER_EDGE_SCROLL_PX) {
      this.viewport.scrollToOffset(
        Math.max(0, this.viewport.measureScrollOffset('top') - REORDER_EDGE_SCROLL_STEP_PX),
      );
    } else if (this.reorderLastY > rect.bottom - REORDER_EDGE_SCROLL_PX) {
      this.viewport.scrollToOffset(this.viewport.measureScrollOffset('top') + REORDER_EDGE_SCROLL_STEP_PX);
    }
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
    this.dropTarget = null;
    this.dropIntoGroupId.set(null);
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
      if (kind === 'favourite') {
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
        if (kind === 'favourite') {
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
      if (kind === 'favourite') {
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
        if (kind === 'favourite') {
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

  // ---------- Groups ----------

  /** Whether any selected request is in a group (offers "Remove from group"). */
  protected readonly selectionInGroup = computed(() => {
    const selected = this.selectedRowIds();
    return this.filteredActiveRequests().some((r) => selected.has(r.id) && r.group_id != null);
  });

  /** A click on a heading picks a collection's group; only its arrow folds it. */
  onGroupClick(entry: GroupEntry): void {
    if (this.suppressHeadingClick) {
      this.suppressHeadingClick = false;
      return;
    }
    this.pickGroup(entry);
  }

  onFoldClick(entry: GroupEntry, event: Event): void {
    event.stopPropagation();
    if (this.suppressHeadingClick) {
      this.suppressHeadingClick = false;
      return;
    }
    this.toggleFold(entry);
  }

  private pickGroup(entry: GroupEntry): void {
    if (entry.groupId === null) return;
    this.clearSelection();
    // Forgotten too, or the refresh after the next command would select those rows again.
    const key = this.activeGroupKey();
    if (key !== null) this.selectionState.clearSelection(key);
    // The inspector shows the group, not the request that was open.
    this.state.selectedRequest.set(null);
    this.state.selectedResponse.set(null);
    this.selectedGroupId.set(entry.groupId);
  }

  onGroupContextMenu(entry: GroupEntry, event: MouseEvent, template: TemplateRef<{ $implicit: GroupEntry }>): void {
    event.preventDefault();
    event.stopPropagation();
    if (entry.groupId === null) return;
    this.pickGroup(entry);
    this.contextMenu.open(template, entry, event.clientX, event.clientY);
  }

  // ---------- Group drag ----------

  private groupDragPending: { groupId: number; label: string; startY: number } | null = null;
  readonly groupDrag = signal<{ groupId: number; label: string } | null>(null);
  private groupTarget: DropTarget | null = null;
  private groupDragFrame: number | null = null;
  /** The click that ends a heading drag must not also fold the heading. */
  private suppressHeadingClick = false;
  private readonly groupGhost = viewChild<ElementRef<HTMLElement>>('groupGhost');
  private readonly groupDropLine = viewChild<ElementRef<HTMLElement>>('groupDropLine');

  onGroupMouseDown(entry: GroupEntry, event: MouseEvent): void {
    this.suppressHeadingClick = false;
    if (event.button !== 0 || entry.groupId === null) return;
    if (!this.canArrange()) return;
    event.preventDefault();
    this.groupDragPending = { groupId: entry.groupId, label: entry.label, startY: event.clientY };
  }

  private handleGroupDragMove(event: MouseEvent): void {
    this.reorderLastX = event.clientX;
    this.reorderLastY = event.clientY;
    const pending = this.groupDragPending;
    if (!pending || this.groupDrag()) return;
    if (Math.abs(event.clientY - pending.startY) <= DRAG_THRESHOLD_PX) return;
    this.groupDrag.set({ groupId: pending.groupId, label: pending.label });
    document.body.classList.add('reorder-dragging');
    const loop = () => {
      this.groupDragFrame = null;
      if (!this.groupDrag()) return;
      this.renderGroupDragFrame();
      this.groupDragFrame = requestAnimationFrame(loop);
    };
    this.groupDragFrame = requestAnimationFrame(loop);
  }

  private renderGroupDragFrame(): void {
    const ghost = this.groupGhost()?.nativeElement;
    const line = this.groupDropLine()?.nativeElement;
    if (!ghost || !line || !this.viewport) return;

    const rect = this.viewport.elementRef.nativeElement.getBoundingClientRect();
    this.autoScrollReorder(rect);
    const offset = this.viewport.measureScrollOffset('top');
    const rows = this.displayRows();
    const target = groupDropTarget(rows, offset + this.reorderLastY - rect.top, this.rowHeight);
    const layout = this.collectionLayout();
    const groupId = this.groupDrag()?.groupId;
    if (groupId !== undefined && sameLayout(moveGroup(layout, groupId, target), layout)) {
      // Dropping here changes nothing, so no line.
      this.groupTarget = null;
      line.style.visibility = 'hidden';
    } else {
      this.groupTarget = target;
      this.placeDropLine(line, rect, target.lineRow ?? 0, offset, false);
    }
    ghost.style.left = `${this.reorderLastX}px`;
    ghost.style.top = `${this.reorderLastY}px`;
    ghost.style.visibility = 'visible';
  }

  private endGroupDrag(): { groupId: number; label: string } | null {
    const drag = this.groupDrag();
    this.groupDragPending = null;
    if (!drag) return null;
    this.groupDrag.set(null);
    if (this.groupDragFrame !== null) cancelAnimationFrame(this.groupDragFrame);
    this.groupDragFrame = null;
    document.body.classList.remove('reorder-dragging');
    this.suppressHeadingClick = true;
    return drag;
  }

  private cancelGroupDrag(): boolean {
    return this.groupDragPending !== null && this.endGroupDrag() !== null;
  }

  private finishGroupDrag(): void {
    const drag = this.endGroupDrag();
    const target = this.groupTarget;
    this.groupTarget = null;
    if (!drag || !target) return;
    this.applyLayout(moveGroup(this.collectionLayout(), drag.groupId, target));
  }

  /** Puts the selected requests in a new group and starts renaming it. */
  async groupSelection(): Promise<void> {
    this.contextMenu.close();
    const collection = this.state.selectedCollection();
    const ids = [...this.selectedRowIds()];
    if (!collection || ids.length === 0) return;
    const name = freeGroupName(this.collectionGroups().map((g) => g.name));
    try {
      const group = await this.groupApi.create(collection.id, name, ids);
      this.clearSelection();
      this.selectedGroupId.set(group.id);
      this.nameToFocus.set({ kind: 'group', id: group.id });
    } catch (err) {
      console.error('grouping requests failed', err);
    }
  }

  async moveSelectionToGroup(groupId: number | null): Promise<void> {
    this.contextMenu.close();
    const ids = [...this.selectedRowIds()];
    if (ids.length === 0) return;
    try {
      await this.groupApi.setRequests(groupId, ids);
    } catch (err) {
      console.error('moving requests to a group failed', err);
    }
  }

  async ungroup(entry: GroupEntry): Promise<void> {
    if (entry.groupId !== null) await this.ungroupById(entry.groupId);
  }

  async ungroupById(groupId: number): Promise<void> {
    this.contextMenu.close();
    if (this.selectedGroupId() === groupId) this.selectedGroupId.set(null);
    try {
      await this.groupApi.delete(groupId);
    } catch (err) {
      console.error('ungrouping failed', err);
    }
  }

  /** Renames from the URL bar's name field; an empty or unchanged name keeps the old one. */
  async renameGroupTo(group: RequestGroupRecord, value: string, field: HTMLInputElement): Promise<void> {
    const name = value.trim();
    if (!name || name === group.name) {
      field.value = group.name;
      return;
    }
    try {
      await this.groupApi.update({ ...group, name });
    } catch (err) {
      console.error('renaming the group failed', err);
    }
  }

  onGroupColorClick(group: RequestGroupRecord, slot: string | null): void {
    const color = slot === group.color ? null : slot;
    this.groupApi.update({ ...group, color }).catch((err) => console.error('colouring the group failed', err));
  }

  /** The heading menu's Rename: the group's name is edited in the toolbar. */
  focusGroupName(groupId: number): void {
    this.contextMenu.close();
    if (this.selectedGroupId() !== groupId) {
      const entry = this.displayRows().find((row) => row.kind === 'group' && row.groupId === groupId);
      if (entry?.kind === 'group') this.pickGroup(entry);
    }
    this.nameToFocus.set({ kind: 'group', id: groupId });
  }

  /** Puts the cursor in the toolbar's name field once it shows the item asked for. */
  private readonly focusNameField = afterRenderEffect(() => {
    const target = this.nameToFocus();
    if (!target) return;
    const shown = target.kind === 'request' ? this.headerRequest()?.id : this.selectedGroup()?.id;
    const field = (target.kind === 'request' ? this.requestNameField() : this.groupNameField())?.nativeElement;
    if (shown !== target.id || !field) return;
    field.focus();
    field.select();
    untracked(() => this.nameToFocus.set(null));
  });

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
      // The copy is the saved request: let the edits already made land first.
      await this.requestApi.whenSaved(req.id);
      const curl = await this.requestApi.createCurl(req.id);
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
    this.nameToFocus.set({ kind: 'request', id: req.id });
    // Scroll once the list has drawn the new row, so the offset isn't clamped
    // to the old list height.
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

  /** Double-click on a row: the request opens in zen mode. */
  openZenMode(req: HttpRequestSummary | HttpBinnedRequestSummary, event: MouseEvent): void {
    // A quick Ctrl/Shift click pair is selecting, not asking for zen mode.
    if (event.ctrlKey || event.metaKey || event.shiftKey) return;
    this.contextMenu.close();
    if (this.state.showingBin()) return;
    this.selectCell(req as HttpRequestSummary, 'name');
    this.state.zenModeOpen.set(true);
  }

  /** Renames from the toolbar's name field; an empty or unchanged name keeps the old one. */
  protected async renameRequestTo(req: HttpRequestSummary, value: string, field: HTMLInputElement): Promise<void> {
    const name = value.trim();
    if (!name || name === req.name) {
      field.value = req.name;
      return;
    }
    await this.updateRequestField(req, 'name', name);
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
      const saved = await this.requestApi.saveFields(req.id, { [field]: value } as Partial<HttpRequest>);
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

const FOLDED_KEY = 'snaprq.folded';

function loadFoldedKeys(): Set<string> {
  try {
    const raw: unknown = JSON.parse(localStorage.getItem(FOLDED_KEY) ?? '[]');
    return new Set(Array.isArray(raw) ? raw.filter((k): k is string => typeof k === 'string') : []);
  } catch {
    return new Set();
  }
}

function saveFoldedKeys(keys: Set<string>): void {
  try {
    localStorage.setItem(FOLDED_KEY, JSON.stringify([...keys]));
  } catch {
    // Storage can be full or disabled; folding still works for this session.
  }
}

function freeGroupName(taken: string[]): string {
  const names = new Set(taken);
  if (!names.has('New group')) return 'New group';
  let n = 2;
  while (names.has(`New group ${n}`)) n++;
  return `New group ${n}`;
}
