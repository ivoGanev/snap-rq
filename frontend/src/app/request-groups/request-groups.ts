import { Component, effect, TemplateRef, ViewChild, computed, inject, isDevMode, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Dialogs } from '@wailsio/runtime';
import { WorkspaceStateService } from '../core/services/workspace-state.service';
import { OpenApiApiService, type OpenApiPreviewCollection } from '../core/services/openapi.service';
import { ToastService } from '../core/services/toast.service';
import { CollectionApiService, type Collection } from '../core/services/collection.service';
import {
  FavouriteApiService,
  type FavouriteCollection,
} from '../core/services/favourite.service';
import { RequestApiService } from '../core/services/request.service';
import { CommandApiService } from '../core/services/command.service';
import { SelectionStateService, groupKey } from '../core/services/selection-state.service';
import { TagApiService, type Tag } from '../core/services/tag.service';
import { DragSessionService, type DropKind } from '../core/services/drag-session.service';
import { ContextMenuService } from '../core/services/context-menu.service';
import { IconManifestService } from '../core/services/icon-manifest.service';
import { ConfirmService } from '../core/services/confirm.service';
import { isEditableTarget } from '../core/dom/editable-target';
import {
  FOLDER_FOR_KIND,
  RequestGroupList,
  sidebarCollectionKey,
  sidebarFavouriteKey,
  sidebarTagKey,
  type SidebarAppearancePatch,
  type SidebarFolder,
  type SidebarItem,
} from '../request-group-list/request-group-list';
import { SidebarRail } from '../sidebar-rail/sidebar-rail';
import {
  SIDEBAR_VIEWS,
  type SidebarView,
  type SidebarViewId,
} from '../sidebar-rail/sidebar-views';

type SidebarItemOfKind<K extends SidebarItem['kind']> = Extract<SidebarItem, { kind: K }>;

/**
 * Per-kind behaviour maps. Each map is keyed exhaustively by item kind, so
 * adding a new kind is a compile error until an entry exists — no scattered
 * if/else chains to drift apart. `kind` discriminates the union, so handlers
 * receive fully typed items without casts.
 */
type SelectHandlerMap = {
  [K in SidebarItem['kind']]: (item: SidebarItemOfKind<K>) => void;
};
type RenameHandlerMap = {
  [K in SidebarItem['kind']]: (item: SidebarItemOfKind<K>, newName: string) => Promise<void>;
};
type SaveAppearanceHandlerMap = {
  [K in SidebarItem['kind']]: (
    item: SidebarItemOfKind<K>,
    patch: SidebarAppearancePatch,
  ) => Promise<void>;
};
type RemoveHandlerMap = {
  [K in SidebarItem['kind']]: (item: SidebarItemOfKind<K>) => Promise<void>;
};

const COLLECTION_COLOR_PALETTE: string[] = [
  '#ef4444',
  '#f97316',
  '#f59e0b',
  '#84cc16',
  '#22c55e',
  '#14b8a6',
  '#06b6d4',
  '#0ea5e9',
  '#3b82f6',
  '#6366f1',
  '#8b5cf6',
  '#a855f7',
  '#d946ef',
  '#f43f5e',
  '#78716c',
  '#374151',
];

/**
 * Left navigation shell. Collapsed mode renders an icon-only rail with an
 * explicit expand control; expanded mode renders the labelled nav plus the
 * active folder's group list. Mode changes are always user-driven.
 */
@Component({
  selector: 'app-request-groups',
  imports: [FormsModule, RequestGroupList, SidebarRail],
  templateUrl: './request-groups.html',
  styleUrl: './request-groups.scss',
  host: {
    class: 'sidebar sidebar-left',
    'aria-label': 'Request groups',
    '(window:keydown.escape)': 'onEscapePressed()',
    '(window:keydown)': 'onWindowKeydown($event)',
    '(window:mousemove)': 'onWindowMouseMove($event)',
  },
})
export class RequestGroups {
  protected readonly state = inject(WorkspaceStateService);
  private readonly collectionApi = inject(CollectionApiService);
  private readonly favouriteApi = inject(FavouriteApiService);
  private readonly requestApi = inject(RequestApiService);
  private readonly selectionState = inject(SelectionStateService);
  protected readonly iconManifest = inject(IconManifestService);
  private readonly tagApi = inject(TagApiService);
  private readonly contextMenu = inject(ContextMenuService);
  private readonly commandApi = inject(CommandApiService);
  private readonly confirm = inject(ConfirmService);
  protected readonly drag = inject(DragSessionService);
  private readonly openapiApi = inject(OpenApiApiService);
  private readonly toast = inject(ToastService);

  @ViewChild('collectionsAddMenu', { static: true })
  private readonly collectionsAddMenu!: TemplateRef<{ $implicit: unknown }>;

  /** Development builds only: the rail shows an undo-history debug button. */
  protected readonly devUndoVisible = isDevMode();

  openUndoPanel(): void {
    this.commandApi.openDevPanel();
  }

  /**
   * Single registry of per-kind behaviour. Adding a new sidebar kind is a
   * compile error until an entry exists here — no scattered if/else to drift.
   */
  private readonly selectHandlers: SelectHandlerMap = {
    collection: (item) => {
      this.state.selectedCollection.set(item.data);
    },
    favourite: (item) => {
      this.state.selectedFavouriteCollection.set(item.data);
    },
    tag: (item) => {
      this.state.selectedTag.set(item.data.name);
    },
  };

  private readonly renameHandlers: RenameHandlerMap = {
    collection: async (item, newName) => {
      await this.collectionApi.update({ ...item.data, name: newName });
    },
    favourite: async (item, newName) => {
      await this.favouriteApi.updateCollection({ ...item.data, name: newName });
    },
    tag: async (item, newName) => {
      const project = this.state.selectedProject();
      if (!project) return;
      await this.tagApi.renameTag(project.id, item.data.name, newName);
      if (this.state.selectedTag() === item.data.name) {
        this.state.selectedTag.set(newName);
      }
    },
  };

  private readonly saveAppearanceHandlers: SaveAppearanceHandlerMap = {
    collection: async (item, patch) => {
      await this.collectionApi.updateAppearance(item.id, patch);
    },
    favourite: async (item, patch) => {
      await this.favouriteApi.updateAppearance(item.id, patch);
    },
    tag: async (item, patch) => {
      await this.tagApi.updateAppearance(item.id, patch);
    },
  };

  private readonly removeHandlers: RemoveHandlerMap = {
    collection: (item) => this.deleteCollection(item.data),
    favourite: (item) => this.deleteFavouriteCollection(item.data),
    tag: (item) => this.deleteTag(item.data),
  };

  // Generic dispatchers: they tie the map key to the item's kind, so the
  // compiler keeps the two correlated without casts or per-kind branches.
  private selectItem<K extends SidebarItem['kind']>(item: SidebarItemOfKind<K>): void {
    this.selectHandlers[item.kind](item);
  }

  private renameItem<K extends SidebarItem['kind']>(
    item: SidebarItemOfKind<K>,
    newName: string,
  ): Promise<void> {
    return this.renameHandlers[item.kind](item, newName);
  }

  private applyAppearance<K extends SidebarItem['kind']>(
    item: SidebarItemOfKind<K>,
    patch: SidebarAppearancePatch,
  ): Promise<void> {
    return this.saveAppearanceHandlers[item.kind](item, patch);
  }

  private removeItem<K extends SidebarItem['kind']>(item: SidebarItemOfKind<K>): Promise<void> {
    return this.removeHandlers[item.kind](item);
  }

  protected readonly collections = this.collectionApi.collections;
  protected readonly favouriteCollections = this.favouriteApi.collections;
  protected readonly allTags = this.tagApi.allTags;

  // Which folder panel is open in the second column.
  protected readonly folderFilter = signal<SidebarFolder | null>(null);

  protected readonly sidebarViews = SIDEBAR_VIEWS;

  private readonly viewSelectHandlers: Record<SidebarViewId, () => void> = {
    all: () => this.selectAll(),
    famous: () => this.selectFamous(),
    collections: () => this.selectCollections(),
    favourites: () => this.selectFavourites(),
    tags: () => this.selectTags(),
    trash: () => this.selectTrashBin(),
  };

  private readonly viewActiveHandlers: Record<SidebarViewId, () => boolean> = {
    all: () => this.state.showingAllRequests(),
    famous: () => this.state.showingFamousRequests(),
    collections: () => this.folderFilter() === 'collections',
    favourites: () => this.folderFilter() === 'favourites',
    tags: () => this.folderFilter() === 'tags',
    trash: () => this.state.showingBin(),
  };

  /** Active entry, used by the collapsed rail to keep its highlight in sync. */
  protected readonly activeView = computed<SidebarViewId>(() => {
    const folder = this.folderFilter();
    if (folder) return folder;
    if (this.state.showingBin()) return 'trash';
    if (this.state.showingFamousRequests()) return 'famous';
    return 'all';
  });

  selectView(id: SidebarViewId): void {
    this.viewSelectHandlers[id]();
  }

  isViewActive(view: SidebarView): boolean {
    return this.viewActiveHandlers[view.id]();
  }

  expandSidebar(): void {
    this.state.sidebarExpanded.set(true);
  }

  collapseSidebar(): void {
    this.state.sidebarExpanded.set(false);
  }

  // Creation popups
  protected readonly newCollectionPopupOpen = signal(false);
  protected readonly newCollectionName = signal('');
  protected readonly newFavouritePopupOpen = signal(false);
  protected readonly newFavouriteName = signal('');
  protected readonly newTagPopupOpen = signal(false);

  // Import dialog: the collections a spec would create, and which are ticked.
  protected readonly importRows = signal<OpenApiPreviewCollection[] | null>(null);
  protected readonly importSelected = signal<ReadonlySet<string>>(new Set());
  private importPath = '';
  protected readonly importableCount = computed(
    () => (this.importRows() ?? []).filter((row) => !row.exists).length,
  );
  protected readonly newTagName = signal('');

  // Rename inline
  protected readonly renamingItem = signal<SidebarItem | null>(null);
  protected readonly renameValue = signal('');

  // Appearance popup
  protected readonly appearancePopupOpen = signal(false);
  protected readonly appearanceTarget = signal<SidebarItem | null>(null);
  protected readonly appearanceTab = signal<'icon' | 'color'>('icon');
  protected readonly collectionColorPalette = COLLECTION_COLOR_PALETTE;

  /** Per-folder adapters from domain models to the sidebar item union. */
  private readonly folderSources: Record<SidebarFolder, () => SidebarItem[]> = {
    collections: () =>
      this.collections().map((c) => ({
        kind: 'collection',
        id: c.id,
        name: c.name,
        appearance: c.appearance,
        data: c,
      })),
    favourites: () =>
      this.favouriteCollections().map((f) => ({
        kind: 'favourite',
        id: f.id,
        name: f.name,
        appearance: f.appearance,
        data: f,
      })),
    tags: () =>
      this.allTags().map((t) => ({
        kind: 'tag',
        id: t.id,
        name: t.name,
        appearance: t.appearance,
        data: t,
      })),
  };

  readonly filteredItems = computed<SidebarItem[]>(() => {
    const filter = this.folderFilter();
    return filter ? this.folderSources[filter]() : [];
  });

  /** Single key describing which sidebar item is selected, in any folder. */
  protected readonly selectedKey = computed<string | null>(() => {
    const collection = this.state.selectedCollection();
    if (collection) return sidebarCollectionKey(collection.id);
    const favourite = this.state.selectedFavouriteCollection();
    if (favourite) return sidebarFavouriteKey(favourite.id);
    const tag = this.state.selectedTag();
    return tag ? sidebarTagKey(tag) : null;
  });

  /** The selected sidebar entity (collection/favourite/tag), if any. */
  protected readonly selectedSidebarItem = computed<SidebarItem | null>(() => {
    const collection = this.state.selectedCollection();
    if (collection) {
      return {
        kind: 'collection',
        id: collection.id,
        name: collection.name,
        appearance: collection.appearance,
        data: collection,
      };
    }

    const favourite = this.state.selectedFavouriteCollection();
    if (favourite) {
      return {
        kind: 'favourite',
        id: favourite.id,
        name: favourite.name,
        appearance: favourite.appearance,
        data: favourite,
      };
    }

    const tagName = this.state.selectedTag();
    if (tagName) {
      const tag = this.allTags().find((t) => t.name === tagName);
      if (tag) {
        return { kind: 'tag', id: tag.id, name: tag.name, appearance: tag.appearance, data: tag };
      }
    }

    return null;
  });

  /** Sidebar key of the item under the pointer during a drag, if any. */
  protected readonly dropTargetKey = computed<string | null>(() => {
    const target = this.drag.hovered();
    if (!target) return null;
    if (target.kind === 'collection') return sidebarCollectionKey(target.id);
    if (target.kind === 'favourite') return sidebarFavouriteKey(target.id);
    return sidebarTagKey(target.name);
  });

  protected readonly dropZoneActive = computed(
    () => this.drag.overEmptyCollections() && this.folderFilter() === 'collections',
  );

  onWindowMouseMove(event: MouseEvent): void {
    if (!this.drag.active()) return;

    const el = document.elementFromPoint(event.clientX, event.clientY) as HTMLElement | null;
    const itemEl = el?.closest<HTMLElement>('[data-item-kind]');
    if (itemEl) {
      const kind = itemEl.dataset['itemKind'] as DropKind;
      const id = Number(itemEl.dataset['itemId'] ?? 0);
      const name = itemEl.dataset['itemName'] ?? '';
      this.drag.setHover({ kind, id, name });
      return;
    }

    if (this.folderFilter() === 'collections' && this.collections().length === 0) {
      const listEl = el?.closest<HTMLElement>('.request-group-list');
      if (listEl) {
        this.drag.setOverEmptyCollections(true);
        return;
      }
    }

    this.drag.setHover(null);
    this.drag.setOverEmptyCollections(false);
  }

  constructor() {
    effect(() => {
      this.state.sidebarListOpen.set(this.folderFilter() !== null);
    });
  }

  onEscapePressed(): void {
    if (this.contextMenu.isOpen()) {
      this.contextMenu.close();
      return;
    }
    if (this.appearancePopupOpen()) {
      this.closeAppearancePopup();
      return;
    }
    if (this.newCollectionPopupOpen()) {
      this.closeNewCollectionPopup();
      return;
    }
    if (this.newFavouritePopupOpen()) {
      this.closeNewFavouritePopup();
      return;
    }
    if (this.newTagPopupOpen()) {
      this.closeNewTagPopup();
      return;
    }
    if (this.renamingItem()) {
      this.cancelRename();
      return;
    }
  }

  /** Delete key: removes the selected sidebar entity (with confirmation). */
  onWindowKeydown(event: KeyboardEvent): void {
    if (event.key !== 'Delete') return;
    if (this.confirm.active() || isEditableTarget(event.target)) return;
    // The request list owns Delete whenever it has a row selection.
    if (this.state.selectedIds().size > 0) return;
    if (
      this.contextMenu.isOpen() ||
      this.renamingItem() ||
      this.appearancePopupOpen() ||
      this.newCollectionPopupOpen() ||
      this.newFavouritePopupOpen() ||
      this.newTagPopupOpen()
    ) {
      return;
    }

    const item = this.selectedSidebarItem();
    if (!item) return;

    event.preventDefault();
    void this.deleteItem(item);
  }

  // ---------- Selection ----------

  selectAll(): void {
    this.state.selectedCollection.set(null);
    this.state.selectedFavouriteCollection.set(null);
    this.state.selectedTag.set(null);
    this.state.exitBinView();
    this.state.showingFamousRequests.set(false);
    this.state.showingAllRequests.set(true);
    this.folderFilter.set(null);
  }

  selectFamous(): void {
    this.state.selectedCollection.set(null);
    this.state.selectedFavouriteCollection.set(null);
    this.state.selectedTag.set(null);
    this.state.exitBinView();
    this.state.showingAllRequests.set(false);
    this.state.showingFamousRequests.set(true);
    this.folderFilter.set(null);
  }

  selectTrashBin(): void {
    const project = this.state.selectedProject();
    if (!project) return;
    this.state.enterBinView();
    this.folderFilter.set(null);
    void this.state.loadBinnedRequests(project.id);
  }

  selectCollections(): void {
    this.toggleFolderFilter('collections');
  }

  selectFavourites(): void {
    this.toggleFolderFilter('favourites');
  }

  selectTags(): void {
    this.toggleFolderFilter('tags');
  }

  private toggleFolderFilter(filter: SidebarFolder): void {
    this.state.exitBinView();
    const current = this.folderFilter();
    const next = current === filter ? null : filter;

    if (next !== null) {
      this.state.showingAllRequests.set(false);
      this.state.showingFamousRequests.set(false);
      // Opening a folder must reveal its list, even from the collapsed rail.
      this.state.sidebarExpanded.set(true);
    }

    this.folderFilter.set(next);
  }

  selectSidebarItem(item: SidebarItem): void {
    this.state.exitBinView();
    this.state.selectedCollection.set(null);
    this.state.selectedFavouriteCollection.set(null);
    this.state.selectedTag.set(null);
    this.state.showingAllRequests.set(false);
    this.state.showingFamousRequests.set(false);
    this.selectItem(item);
    this.folderFilter.set(FOLDER_FOR_KIND[item.kind]);
  }

  // ---------- Add ----------

  onAddButtonClick(): void {
    this.contextMenu.close();
    switch (this.folderFilter()) {
      case 'collections':
        this.openNewCollectionPopup();
        break;
      case 'favourites':
        this.openNewFavouritePopup();
        break;
      case 'tags':
        this.openNewTagPopup();
        break;
    }
  }

  /**
   * Opens the "+" menu through the shared context menu, which is drawn at the
   * root of the app. A menu drawn inside the sidebar would be clipped and
   * positioned by it (the sidebar has a backdrop-filter and overflow: hidden).
   * It opens on the next tick so the click that asked for it, and the outlet's
   * document click listener, are finished first.
   */
  openCollectionsAddMenu(position: { x: number; y: number }): void {
    setTimeout(() => this.contextMenu.open(this.collectionsAddMenu, null, position.x, position.y), 0);
  }

  onCollectionAddMenu(action: 'new' | 'import' | 'export'): void {
    this.contextMenu.close();
    switch (action) {
      case 'new':
        this.openNewCollectionPopup();
        break;
      case 'import':
        void this.importOpenApi();
        break;
      case 'export':
        void this.exportOpenApi();
        break;
    }
  }

  async importOpenApi(): Promise<void> {
    const project = this.state.selectedProject();
    if (!project) return;

    const path = await Dialogs.OpenFile({
      Title: 'Import OpenAPI',
      AllowsMultipleSelection: false,
    });
    if (typeof path !== 'string' || !path) return;

    try {
      const rows = await this.openapiApi.previewSpec(project.id, path);
      this.importPath = path;
      this.importSelected.set(new Set(rows.filter((row) => !row.exists).map((row) => row.name)));
      this.importRows.set(rows);
    } catch (err) {
      console.error(err);
      this.toast.show(`Import failed: ${errorText(err)}`);
    }
  }

  closeImportDialog(): void {
    this.importRows.set(null);
  }

  toggleImportRow(row: OpenApiPreviewCollection): void {
    if (row.exists) return;
    const next = new Set(this.importSelected());
    if (!next.delete(row.name)) next.add(row.name);
    this.importSelected.set(next);
  }

  setAllImportRows(selected: boolean): void {
    const rows = this.importRows() ?? [];
    this.importSelected.set(
      new Set(selected ? rows.filter((row) => !row.exists).map((row) => row.name) : []),
    );
  }

  async confirmImport(): Promise<void> {
    const project = this.state.selectedProject();
    const names = [...this.importSelected()];
    if (!project || names.length === 0) return;

    this.closeImportDialog();
    try {
      const result = await this.openapiApi.importSpec(project.id, this.importPath, names);
      this.toast.show(
        `Imported ${result.requests_created} requests into ${result.collections_created} collections`,
      );
    } catch (err) {
      console.error(err);
      this.toast.show(`Import failed: ${errorText(err)}`);
    }
  }

  async exportOpenApi(): Promise<void> {
    const project = this.state.selectedProject();
    if (!project) return;

    const path = await Dialogs.SaveFile({
      Filename: 'openapi.json',
      Title: 'Export OpenAPI',
      ButtonText: 'Save',
    });
    if (typeof path !== 'string' || !path) return;

    try {
      const result = await this.openapiApi.exportSpec(project.id, path);
      this.toast.show(
        `Exported ${result.requests_exported} requests across ${result.collections_exported} collections`,
      );
    } catch (err) {
      console.error(err);
      this.toast.show(`Export failed: ${errorText(err)}`);
    }
  }

  openNewCollectionPopup(): void {
    this.contextMenu.close();
    this.newCollectionName.set('');
    this.newCollectionPopupOpen.set(true);
  }

  closeNewCollectionPopup(): void {
    this.newCollectionPopupOpen.set(false);
    if (this.drag.createCollectionRequested()) {
      this.drag.cancelCreateCollection();
    }
  }

  openNewFavouritePopup(): void {
    this.contextMenu.close();
    this.newFavouriteName.set('');
    this.newFavouritePopupOpen.set(true);
  }

  closeNewFavouritePopup(): void {
    this.newFavouritePopupOpen.set(false);
  }

  openNewTagPopup(): void {
    this.contextMenu.close();
    this.newTagName.set('');
    this.newTagPopupOpen.set(true);
  }

  closeNewTagPopup(): void {
    this.newTagPopupOpen.set(false);
  }

  // ---------- Creation ----------

  async addCollection(): Promise<void> {
    const name = this.newCollectionName().trim();
    if (!name) return;

    if (this.drag.createCollectionRequested()) {
      await this.drag.confirmCreateCollection(name);
      this.closeNewCollectionPopup();
      return;
    }

    const project = this.state.selectedProject();
    if (!project) return;

    this.state.loading.set(true);
    try {
      await this.collectionApi.create({
        project_id: project.id,
        name,
      });
      await this.collectionApi.loadForProject(project.id);
      this.closeNewCollectionPopup();
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }

  async addFavouriteCollection(): Promise<void> {
    const project = this.state.selectedProject();
    if (!project) return;

    const name = this.newFavouriteName().trim();
    if (!name) return;

    try {
      await this.favouriteApi.createCollection({ project_id: project.id, name });
      this.closeNewFavouritePopup();
    } catch (err) {
      console.error(err);
    }
  }

  async addTag(): Promise<void> {
    const project = this.state.selectedProject();
    const name = this.newTagName().trim().toLowerCase();
    if (!project || !name) return;

    try {
      await this.tagApi.addTagToRequest(0, project.id, name);
      this.closeNewTagPopup();
    } catch (err) {
      console.error(err);
    }
  }

  // ---------- Context menu ----------

  openItemContextMenu(
    item: SidebarItem,
    event: MouseEvent,
    template: TemplateRef<{ $implicit: SidebarItem }>,
  ): void {
    event.preventDefault();
    event.stopPropagation();
    this.contextMenu.open(template, item, event.clientX, event.clientY);
  }

  // ---------- Rename ----------
  startRename(item: SidebarItem): void {
    this.contextMenu.close();
    this.renameValue.set(item.name);
    this.renamingItem.set(item);
  }

  cancelRename(): void {
    this.renamingItem.set(null);
    this.renameValue.set('');
  }

  async saveRename(event?: Event): Promise<void> {
    event?.preventDefault();
    const item = this.renamingItem();
    if (!item) return;

    const newName = this.renameValue().trim();
    if (!newName || newName === item.name) {
      this.cancelRename();
      return;
    }

    try {
      await this.renameItem(item, newName);
    } catch (err) {
      console.error(err);
    } finally {
      this.cancelRename();
    }
  }

  // ---------- Appearance ----------

  openAppearancePopup(item: SidebarItem, event?: MouseEvent): void {
    event?.stopPropagation();
    this.contextMenu.close();
    this.appearanceTarget.set(item);
    this.appearanceTab.set('icon');
    this.appearancePopupOpen.set(true);
  }

  closeAppearancePopup(): void {
    this.appearancePopupOpen.set(false);
    this.appearanceTarget.set(null);
  }

  async selectAppearanceIcon(iconId: string): Promise<void> {
    await this.saveAppearance({ appearance_type: 'icon', appearance_value: iconId });
    this.closeAppearancePopup();
  }

  async selectAppearanceColor(color: string): Promise<void> {
    await this.saveAppearance({ appearance_type: 'color', appearance_value: color });
    this.closeAppearancePopup();
  }

  async resetAppearance(): Promise<void> {
    await this.saveAppearance({ appearance_type: 'icon', appearance_value: this.iconManifest.defaultIconId() });
    this.closeAppearancePopup();
  }

  private async saveAppearance(patch: SidebarAppearancePatch): Promise<void> {
    const item = this.appearanceTarget();
    if (!item) return;

    try {
      await this.applyAppearance(item, patch);
    } catch (err) {
      console.error(err);
    }
  }

  onAppearanceColorInput(event: Event): void {
    const value = (event.target as HTMLInputElement).value;
    void this.selectAppearanceColor(value);
  }

  // ---------- Delete ----------

  async deleteItem(item: SidebarItem): Promise<void> {
    this.contextMenu.close();

    const noun =
      item.kind === 'collection' ? 'collection' : item.kind === 'favourite' ? 'favourites' : 'tag';
    const confirmed = await this.confirm.ask({
      title: `Delete ${noun}?`,
      message: this.deleteItemMessage(item),
      confirmLabel: 'Delete',
      danger: true,
    });
    if (!confirmed) return;

    try {
      await this.removeItem(item);
    } catch (err) {
      console.error(err);
    }
  }

  private deleteItemMessage(item: SidebarItem): string {
    switch (item.kind) {
      case 'collection':
        return `This will delete "${item.name}" and all requests inside it. This cannot be undone.`;
      case 'favourite':
        return `This will delete the favourites group "${item.name}". The requests themselves are not affected.`;
      case 'tag':
        return `This will delete the tag "${item.name}". The requests themselves are not affected.`;
    }
  }

  private async deleteCollection(collection: Collection): Promise<void> {
    this.state.loading.set(true);
    try {
      const deletedRequestIds = await this.collectionApi.delete(collection.id);
      this.requestApi.removeMany(deletedRequestIds);
      for (const requestId of deletedRequestIds) {
        this.selectionState.deleteRequest(requestId);
      }
      this.selectionState.clearSelection(groupKey.collection(collection.id));

      if (this.state.selectedCollection()?.id === collection.id) {
        this.state.selectedCollection.set(null);
        this.state.selectedRequest.set(null);
        this.state.selectedResponse.set(null);
      }
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }

  private async deleteFavouriteCollection(collection: FavouriteCollection): Promise<void> {
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

  private async deleteTag(tag: Tag): Promise<void> {
    const project = this.state.selectedProject();
    if (!project) return;

    try {
      await this.tagApi.deleteTag(project.id, tag.name);
      if (this.state.selectedTag() === tag.name) {
        this.state.selectedTag.set(null);
      }
    } catch (err) {
      console.error(err);
    }
  }

  // ---------- Helpers ----------

  isAppearanceSelected(appearance: SidebarItem['appearance'], type: 'icon' | 'color', value: string): boolean {
    return appearance.appearance_type === type && appearance.appearance_value === value;
  }

  /** Icons available in the appearance popup for the targeted item's pack. */
  appearanceIconEntries() {
    const target = this.appearanceTarget();
    return target ? this.iconManifest.packIcons(FOLDER_FOR_KIND[target.kind]) : [];
  }
}

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
