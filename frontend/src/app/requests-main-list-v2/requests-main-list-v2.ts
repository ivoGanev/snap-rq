import { Component, computed, effect, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { WorkspaceStateService } from '../core/services/workspace-state.service';
import { RequestApiService, type HttpRequest } from '../core/services/request.service';
import { FavouriteApiService, type FavouriteCollection } from '../core/services/favourite.service';
import { CollectionApiService, type Collection } from '../core/services/collection.service';
import { SelectionStateService } from '../core/services/selection-state.service';
import { TagApiService, type Tag } from '../core/services/tag.service';

type ColumnKey = 'name' | 'url' | 'method' | 'headers' | 'body' | 'tags' | 'favourites';
type SortDirection = 'asc' | 'desc';

interface SelectedCell {
  requestId: number;
  column: ColumnKey;
}

const COLUMNS: { key: ColumnKey; label: string }[] = [
  { key: 'name', label: 'Name' },
  { key: 'url', label: 'URL' },
  { key: 'method', label: 'Method' },
  { key: 'headers', label: 'Headers' },
  { key: 'body', label: 'Body' },
  { key: 'tags', label: 'Tags' },
  { key: 'favourites', label: 'Favourites' },
];

const HTTP_METHODS = ['GET', 'POST', 'PUT', 'DELETE', 'PATCH', 'HEAD', 'OPTIONS'];
const DRAG_THRESHOLD_PX = 5;

/**
 * Spreadsheet-style experimental request list with multi-select and bulk actions.
 */
@Component({
  selector: 'app-requests-main-list-v2',
  imports: [FormsModule],
  templateUrl: './requests-main-list-v2.html',
  styleUrl: './requests-main-list-v2.scss',
  host: {
    class: 'main-column',
    'aria-label': 'Requests V2',
    '(window:keydown.escape)': 'onEscapePressed()',
    '(window:mousemove)': 'onWindowMouseMove($event)',
    '(window:mouseup)': 'onWindowMouseUp()',
  },
})
export class RequestsMainListV2 {
  protected readonly state = inject(WorkspaceStateService);
  private readonly requestApi = inject(RequestApiService);
  private readonly favouriteApi = inject(FavouriteApiService);
  private readonly collectionApi = inject(CollectionApiService);
  private readonly selectionState = inject(SelectionStateService);
  private readonly tagApi = inject(TagApiService);

  protected readonly columns = COLUMNS;
  protected readonly httpMethods = HTTP_METHODS;
  protected readonly requestTags = this.tagApi.requestTags;
  protected readonly favouriteCollections = this.favouriteApi.collections;
  protected readonly favouriteMembership = this.favouriteApi.membership;
  protected readonly requestFavouriteIds = this.favouriteApi.requestsMembership;
  protected readonly collections = this.collectionApi.collections;

  readonly requestSearchQuery = signal('');
  readonly tagRequests = signal<HttpRequest[]>([]);
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
  readonly editModalRequest = signal<HttpRequest | null>(null);
  readonly editModalValue = signal('');

  readonly methodModalOpen = signal(false);
  readonly methodModalRequest = signal<HttpRequest | null>(null);
  readonly methodModalValue = signal('GET');

  readonly tagsModalOpen = signal(false);
  readonly tagsModalRequest = signal<HttpRequest | null>(null);
  readonly tagsModalNewTagName = signal('');

  readonly favouritesModalOpen = signal(false);
  readonly favouritesModalRequest = signal<HttpRequest | null>(null);
  readonly newFavouriteName = signal('');

  // Bulk / context menu
  readonly contextMenuOpen = signal(false);
  readonly contextMenuX = signal(0);
  readonly contextMenuY = signal(0);

  readonly moveCollectionModalOpen = signal(false);
  readonly bulkTagModalOpen = signal(false);
  readonly bulkTagName = signal('');
  readonly bulkFavouritesModalOpen = signal(false);

  private loadVersion = 0;

  readonly activeRequests = computed<HttpRequest[]>(() => {
    if (this.state.selectedTag()) {
      return this.tagRequests();
    }
    if (this.state.selectedFavouriteCollection()) {
      return this.favouriteApi.requests();
    }
    if (this.state.selectedCollection()) {
      return this.requestApi.requests();
    }
    return [];
  });

  readonly activeGroupName = computed<string | null>(() => {
    const tag = this.state.selectedTag();
    if (tag) return tag;
    const favourite = this.state.selectedFavouriteCollection();
    if (favourite) return favourite.name;
    const collection = this.state.selectedCollection();
    if (collection) return collection.name;
    return null;
  });

  readonly filteredActiveRequests = computed<HttpRequest[]>(() => {
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

  readonly selectedRequest = computed<HttpRequest | null>(() => {
    const cell = this.selectedCell();
    if (!cell) return null;
    return this.activeRequests().find((r) => r.id === cell.requestId) ?? null;
  });

  readonly selectedRequests = computed<HttpRequest[]>(() => {
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

      const version = ++this.loadVersion;
      this.requestSearchQuery.set('');
      this.clearSelection();
      this.state.selectedRequest.set(null);

      void this.loadActiveGroup(version, collection?.id ?? null, favourite?.id ?? null, tag);
    });
  }

  private async loadActiveGroup(
    version: number,
    collectionId: number | null,
    favouriteId: number | null,
    tag: string | null,
  ): Promise<void> {
    try {
      if (tag) {
        const requests = await this.tagApi.getRequestsForTag(tag);
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
        await Promise.all([
          this.tagApi.loadTagsForRequests(requests),
          this.favouriteApi.loadMembershipForRequests(requests),
        ]);
        const rememberedId = this.selectionState.getSelectedRequestForFavourite(favouriteId);
        this.restoreRememberedRequest(requests, rememberedId);
        return;
      }

      if (collectionId !== null) {
        await this.requestApi.loadForCollection(collectionId);
        if (version !== this.loadVersion) return;
        const requests = this.requestApi.requests();
        await Promise.all([
          this.tagApi.loadTagsForRequests(requests),
          this.favouriteApi.loadMembershipForRequests(requests),
        ]);
        const rememberedId = this.selectionState.getSelectedRequestForCollection(collectionId);
        this.restoreRememberedRequest(requests, rememberedId);
        return;
      }

      this.tagRequests.set([]);
      this.favouriteApi.requestsMembership.set({});
    } catch (err) {
      console.error(err);
    }
  }

  private restoreRememberedRequest(requests: HttpRequest[], rememberedId: number | null): void {
    if (rememberedId === null) return;
    const remembered = requests.find((r) => r.id === rememberedId);
    if (remembered) {
      this.selectCell(remembered, 'name');
    }
  }

  onEscapePressed(): void {
    if (this.contextMenuOpen()) {
      this.closeContextMenu();
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

  selectCell(req: HttpRequest, column: ColumnKey): void {
    this.selectedRowIds.set(new Set([req.id]));
    this.selectedCell.set({ requestId: req.id, column });
    this.state.selectedRequest.set(req);
    this.state.rightPanelMode.set('response');

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

  isRowSelected(req: HttpRequest): boolean {
    return this.selectedRowIds().has(req.id);
  }

  isCellSelected(req: HttpRequest, column: ColumnKey): boolean {
    if (this.selectedRowIds().size > 1) return false;
    const cell = this.selectedCell();
    return cell?.requestId === req.id && cell.column === column;
  }

  onRowClick(req: HttpRequest, event: MouseEvent, column: ColumnKey): void {
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

  private toggleRowSelection(req: HttpRequest, column: ColumnKey): void {
    const set = new Set(this.selectedRowIds());
    if (set.has(req.id)) {
      set.delete(req.id);
    } else {
      set.add(req.id);
    }
    this.selectedRowIds.set(set);
    this.selectedCell.set({ requestId: req.id, column });
    this.lastClickedRowId.set(req.id);
    this.state.selectedRequest.set(set.has(req.id) ? req : this.activeRequests().find((r) => set.has(r.id)) ?? null);
  }

  private selectRangeTo(req: HttpRequest, column: ColumnKey): void {
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
    this.state.selectedRequest.set(req);
  }

  // ---------- Drag selection ----------

  onRowMouseDown(req: HttpRequest, event: MouseEvent): void {
    if (event.button !== 0) return;
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
          this.state.selectedRequest.set(startReq);
        }
      }
    }
  }

  onRowMouseEnter(req: HttpRequest): void {
    if (!this.isDragging()) return;
    this.selectedRowIds.update((set) => {
      const next = new Set(set);
      next.add(req.id);
      return next;
    });
    this.selectedCell.set({ requestId: req.id, column: 'name' });
    this.state.selectedRequest.set(req);
  }

  onWindowMouseUp(): void {
    if (this.dragStartRowId() === null) return;

    if (this.isDragging() || this.dragHasMoved()) {
      this.ignoreNextClick.set(true);
    }

    this.dragStartRowId.set(null);
    this.dragStartClientY.set(0);
    this.isDragging.set(false);
    this.dragHasMoved.set(false);
  }

  // ---------- Context menu ----------

  onRowContextMenu(req: HttpRequest, event: MouseEvent): void {
    event.preventDefault();
    event.stopPropagation();

    if (!this.selectedRowIds().has(req.id)) {
      this.selectCell(req, 'name');
      this.lastClickedRowId.set(req.id);
    }

    this.contextMenuX.set(event.clientX);
    this.contextMenuY.set(event.clientY);
    this.contextMenuOpen.set(true);
  }

  closeContextMenu(): void {
    this.contextMenuOpen.set(false);
  }

  async deleteSelectedRequests(): Promise<void> {
    const ids = [...this.selectedRowIds()];
    if (ids.length === 0) return;

    this.closeContextMenu();
    this.state.loading.set(true);

    try {
      const favourite = this.state.selectedFavouriteCollection();
      const collection = this.state.selectedCollection();
      const tag = this.state.selectedTag();

      if (favourite) {
        for (const id of ids) {
          await this.favouriteApi.removeRequest(favourite.id, id);
        }
        await this.favouriteApi.loadRequestsForCollection(favourite.id);
      } else {
        for (const id of ids) {
          await this.requestApi.delete(id);
          this.selectionState.deleteRequest(id);
        }
        if (collection) {
          await this.requestApi.loadForCollection(collection.id);
        }
      }

      if (this.state.selectedRequest() && ids.includes(this.state.selectedRequest()!.id)) {
        this.state.selectedRequest.set(null);
        this.state.selectedResponse.set(null);
        this.state.rightPanelMode.set('response');
      }

      this.clearSelection();

      if (tag) {
        const requests = await this.tagApi.getRequestsForTag(tag);
        this.tagRequests.set(requests);
        await this.tagApi.loadTagsForRequests(requests);
      }
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }

  // ---------- Move to Collection ----------

  openMoveCollectionModal(): void {
    this.closeContextMenu();
    this.moveCollectionModalOpen.set(true);
  }

  closeMoveCollectionModal(): void {
    this.moveCollectionModalOpen.set(false);
  }

  async moveSelectedRequestsToCollection(collection: Collection): Promise<void> {
    const requests = this.selectedRequests();
    if (requests.length === 0) return;

    this.state.loading.set(true);
    try {
      for (const req of requests) {
        await this.requestApi.update({ ...req, collection_id: collection.id });
      }

      const currentCollection = this.state.selectedCollection();
      const currentFavourite = this.state.selectedFavouriteCollection();
      const tag = this.state.selectedTag();

      if (currentCollection) {
        await this.requestApi.loadForCollection(currentCollection.id);
      }
      if (currentFavourite) {
        await this.favouriteApi.loadRequestsForCollection(currentFavourite.id);
      }
      if (tag) {
        const tagRequests = await this.tagApi.getRequestsForTag(tag);
        this.tagRequests.set(tagRequests);
        await this.tagApi.loadTagsForRequests(tagRequests);
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
    this.closeContextMenu();
    this.bulkTagName.set('');
    this.bulkTagModalOpen.set(true);
  }

  closeBulkTagModal(): void {
    this.bulkTagModalOpen.set(false);
    this.bulkTagName.set('');
  }

  async addBulkTag(tagName: string): Promise<void> {
    const name = tagName.trim();
    if (!name) return;

    const requests = this.selectedRequests();
    if (requests.length === 0) return;

    this.state.loading.set(true);
    try {
      for (const req of requests) {
        await this.tagApi.addTagToRequest(req.id, name);
      }
      await this.tagApi.loadTagsForRequests(requests);
      this.bulkTagName.set('');
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }

  // ---------- Bulk Favourites ----------

  openBulkFavouritesModal(): void {
    this.closeContextMenu();
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
    const requests = this.selectedRequests();
    if (requests.length === 0) return false;
    return requests.every((req) => (this.requestFavouriteIds()[req.id] ?? []).includes(collectionId));
  }

  async toggleBulkFavouriteMembership(collection: FavouriteCollection): Promise<void> {
    const requests = this.selectedRequests();
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

      await this.favouriteApi.loadMembershipForRequests(requests);

      const currentFavourite = this.state.selectedFavouriteCollection();
      if (currentFavourite?.id === collection.id) {
        await this.favouriteApi.loadRequestsForCollection(currentFavourite.id);
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

  private compareRequests(a: HttpRequest, b: HttpRequest, column: ColumnKey): number {
    const dir = this.sortDirection() === 'asc' ? 1 : -1;

    switch (column) {
      case 'name':
        return a.name.localeCompare(b.name) * dir;
      case 'url':
        return a.url.localeCompare(b.url) * dir;
      case 'method':
        return a.method.localeCompare(b.method) * dir;
      case 'headers':
        return a.request_headers.localeCompare(b.request_headers) * dir;
      case 'body':
        return a.body.localeCompare(b.body) * dir;
      case 'tags': {
        const aTags = (this.requestTags()[a.id] ?? []).join(', ');
        const bTags = (this.requestTags()[b.id] ?? []).join(', ');
        return aTags.localeCompare(bTags) * dir;
      }
      case 'favourites': {
        const aCount = this.requestFavouriteIds()[a.id]?.length ?? 0;
        const bCount = this.requestFavouriteIds()[b.id]?.length ?? 0;
        if (aCount !== bCount) return (aCount - bCount) * dir;
        return (a.id - b.id) * dir;
      }
      default:
        return 0;
    }
  }

  getCellValue(req: HttpRequest, column: ColumnKey): string {
    switch (column) {
      case 'name':
        return req.name;
      case 'url':
        return req.url;
      case 'method':
        return req.method;
      case 'headers':
        return req.request_headers;
      case 'body':
        return req.body;
      case 'tags':
        return (this.requestTags()[req.id] ?? []).join(', ');
      case 'favourites':
        return '';
      default:
        return '';
    }
  }

  getFavouriteCount(req: HttpRequest): number {
    return this.requestFavouriteIds()[req.id]?.length ?? 0;
  }

  // ---------- New request ----------

  openNewRequestPopup(): void {
    this.newRequestName.set('My new snappy API');
    this.newRequestUrl.set('');
    this.newRequestMethod.set('GET');
    this.newRequestPopupOpen.set(true);
  }

  closeNewRequestPopup(): void {
    this.newRequestPopupOpen.set(false);
  }

  async addRequest(): Promise<void> {
    const collection = this.state.selectedCollection();
    if (!collection) return;

    const name = this.newRequestName().trim();
    if (!name) return;

    this.state.loading.set(true);
    try {
      await this.requestApi.create({
        collection_id: collection.id,
        name,
        url: this.newRequestUrl().trim(),
        method: this.newRequestMethod(),
        body: '',
        request_headers: '',
        status_code: 0,
        response_id: 0,
      });
      await this.requestApi.loadForCollection(collection.id);
      this.closeNewRequestPopup();
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }

  async sendSelectedRequest(event: MouseEvent): Promise<void> {
    const req = this.selectedRequest();
    if (!req) return;
    await this.state.sendRequest(req, event);
  }

  openZenMode(req: HttpRequest, event: MouseEvent): void {
    event.stopPropagation();
    this.selectCell(req, 'name');
    this.state.zenModeOpen.set(true);
  }

  onCellDoubleClick(req: HttpRequest, column: ColumnKey): void {
    if (column === 'method') {
      this.openMethodModal(req);
      return;
    }
    if (column === 'tags') {
      this.openTagsModal(req);
      return;
    }
    if (column === 'favourites') {
      this.openFavouritesModal(req);
      return;
    }
    this.openEditModal(req, column);
  }

  // ---------- Text edit modal ----------

  openEditModal(req: HttpRequest, column: ColumnKey): void {
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

    if (column === 'tags' || column === 'favourites') return;

    const field = this.columnToRequestField(column);
    await this.updateRequestField(req, field, this.editModalValue());
    this.closeEditModal();
  }

  // ---------- Method modal ----------

  openMethodModal(req: HttpRequest): void {
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

  openTagsModal(req: HttpRequest): void {
    this.tagsModalRequest.set(req);
    this.tagsModalNewTagName.set('');
    this.tagsModalOpen.set(true);
  }

  closeTagsModal(): void {
    this.tagsModalOpen.set(false);
    this.tagsModalRequest.set(null);
    this.tagsModalNewTagName.set('');
  }

  async addTagToRequest(req: HttpRequest, tagName: string): Promise<void> {
    const name = tagName.trim();
    if (!name) return;

    try {
      await this.tagApi.addTagToRequest(req.id, name);
      this.tagsModalNewTagName.set('');
    } catch (err) {
      console.error(err);
    }
  }

  async removeTagFromRequest(req: HttpRequest, tagName: string, event: MouseEvent): Promise<void> {
    event.stopPropagation();
    try {
      await this.tagApi.removeTagFromRequest(req.id, tagName);
    } catch (err) {
      console.error(err);
    }
  }

  // ---------- Favourites modal (single request) ----------

  openFavouritesModal(req: HttpRequest): void {
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
      if (this.state.selectedFavouriteCollection()?.id === collection.id) {
        await this.favouriteApi.loadRequestsForCollection(collection.id);
      }
      await this.favouriteApi.loadMembershipForRequests([req]);
    } catch (err) {
      console.error(err);
    }
  }

  async addFavouriteCollection(): Promise<void> {
    const name = this.newFavouriteName().trim();
    const profile = this.state.selectedProject();
    if (!name || !profile) return;

    try {
      await this.favouriteApi.createCollection({ profile_id: profile.profile_id, name });
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

  private columnToRequestField(column: ColumnKey): keyof HttpRequest {
    switch (column) {
      case 'url':
        return 'url';
      case 'method':
        return 'method';
      case 'headers':
        return 'request_headers';
      case 'body':
        return 'body';
      case 'name':
      default:
        return 'name';
    }
  }

  private async updateRequestField<K extends keyof HttpRequest>(
    req: HttpRequest,
    field: K,
    value: HttpRequest[K],
  ): Promise<void> {
    const updated = { ...req, [field]: value } as HttpRequest;

    this.state.loading.set(true);
    try {
      await this.requestApi.update({ ...updated, collection_id: req.collection_id });
      this.patchRequestInLists(updated);

      const selected = this.state.selectedRequest();
      if (selected?.id === updated.id) {
        this.state.selectedRequest.set(updated);
      }
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }

  private patchRequestInLists(updated: HttpRequest): void {
    const patch = (list: HttpRequest[]) => {
      const index = list.findIndex((r) => r.id === updated.id);
      if (index === -1) return list;
      const next = [...list];
      next[index] = updated;
      return next;
    };

    this.requestApi.requests.update(patch);
    this.favouriteApi.requests.update(patch);
    this.tagRequests.update(patch);
  }
}
