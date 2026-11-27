import { Component, TemplateRef, ViewChild, computed, inject, isDevMode, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Dialogs } from '@wailsio/runtime';
import { WorkspaceStateService } from '../core/services/workspace-state.service';
import { OpenApiApiService } from '../core/services/openapi.service';
import { OpenApiImportPrompt } from '../core/services/openapi-import-prompt.service';
import { ToastService } from '../core/services/toast.service';
import { CollectionApiService, type Collection } from '../core/services/collection.service';
import {
  FavouriteApiService,
  type FavouriteCollection,
} from '../core/services/favourite.service';
import { RequestApiService } from '../core/services/request.service';
import { CommandApiService } from '../core/services/command.service';
import { SelectionStateService, groupKey } from '../core/services/selection-state.service';
import { ScriptApiService, type Script } from '../core/services/script.service';
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
  sidebarScriptKey,
  type SidebarAppearancePatch,
  type SidebarFolder,
  type SidebarItem,
} from '../request-group-list/request-group-list';
import { SmartViewList } from '../smart-view-list/smart-view-list';
import {
  AllRequestsView,
  CollectionView,
  FailingView,
  FavouriteView,
  MostUsedView,
  RecentView,
  ScriptView,
  UnusedView,
  type SmartView,
} from '../core/workspace/workspace-view';
import { SIDEBAR_TABS } from './sidebar-views';
import {
  SidebarMemoryService,
  type SidebarTabId,
  type SmartViewId,
} from '../core/services/sidebar-memory.service';

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

const COLOR_PALETTE: string[] = ['#ef4444', '#f59e0b', '#22c55e', '#3b82f6', '#8b5cf6', '#ec4899'];

@Component({
  selector: 'app-request-groups',
  imports: [FormsModule, RequestGroupList, SmartViewList],
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
  private readonly scriptApi = inject(ScriptApiService);
  private readonly contextMenu = inject(ContextMenuService);
  private readonly commandApi = inject(CommandApiService);
  private readonly confirm = inject(ConfirmService);
  protected readonly drag = inject(DragSessionService);
  private readonly openapiApi = inject(OpenApiApiService);
  private readonly importPrompt = inject(OpenApiImportPrompt);
  private readonly toast = inject(ToastService);
  private readonly memory = inject(SidebarMemoryService);


  /** Development builds only: the toolbar shows an undo-history debug button. */
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
      this.state.selectView(new CollectionView(item.data));
    },
    favourite: (item) => {
      this.state.selectView(new FavouriteView(item.data));
    },
    script: (item) => {
      this.state.selectView(new ScriptView(item.data));
    },
  };

  private readonly renameHandlers: RenameHandlerMap = {
    collection: async (item, newName) => {
      await this.collectionApi.update({ ...item.data, name: newName });
    },
    favourite: async (item, newName) => {
      await this.favouriteApi.updateCollection({ ...item.data, name: newName });
    },
    script: async (item, newName) => {
      const project = this.state.selectedProject();
      if (!project) return;
      await this.scriptApi.renameScript(project.id, item.data.name, newName);
      const view = this.state.view();
      if (view?.kind === 'script' && view.name === item.data.name) {
        this.state.selectView(new ScriptView({ ...item.data, name: newName }, view.tab));
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
    script: async (item, patch) => {
      await this.scriptApi.updateAppearance(item.id, patch);
    },
  };

  private readonly removeHandlers: RemoveHandlerMap = {
    collection: (item) => this.deleteCollection(item.data),
    favourite: (item) => this.deleteFavouriteCollection(item.data),
    script: (item) => this.deleteScript(item.data),
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
  protected readonly allScripts = this.scriptApi.allScripts;

  protected readonly sidebarTab = this.memory.tab;
  protected readonly sidebarTabs = SIDEBAR_TABS;

  protected readonly folderFilter = computed<SidebarFolder | null>(() => {
    const tab = this.sidebarTab();
    return tab === 'smart' || tab === 'trash' ? null : tab;
  });

  private readonly smartViewFactories: Record<SmartViewId, () => void> = {
    all: () => this.selectAll(),
    recent: () => this.selectSmartView(new RecentView()),
    usage: () => this.selectSmartView(new MostUsedView()),
    failing: () => this.selectSmartView(new FailingView()),
    unused: () => this.selectSmartView(new UnusedView()),
  };

  protected readonly activeSmartView = computed<SmartViewId | null>(() => {
    if (this.state.showingAllRequests()) return 'all';
    return this.state.smartView()?.kind ?? null;
  });

  selectSmartViewById(id: SmartViewId): void {
    this.smartViewFactories[id]();
  }

  selectTab(id: SidebarTabId): void {
    this.memory.openTab(id);
  }

  // Creation popups
  protected readonly newCollectionPopupOpen = signal(false);
  protected readonly newCollectionName = signal('');

  // Rename inline
  protected readonly renamingItem = signal<SidebarItem | null>(null);
  protected readonly renameValue = signal('');

  protected readonly colorPalette = COLOR_PALETTE;

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
    scripts: () =>
      this.allScripts().map((t) => ({
        kind: 'script',
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
    const script = this.state.selectedScript();
    return script ? sidebarScriptKey(script) : null;
  });

  /** The selected sidebar entity (collection/favourite/script), if any. */
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

    const scriptName = this.state.selectedScript();
    if (scriptName) {
      const script = this.allScripts().find((t) => t.name === scriptName);
      if (script) {
        return { kind: 'script', id: script.id, name: script.name, appearance: script.appearance, data: script };
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
    return sidebarScriptKey(target.name);
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

  onEscapePressed(): void {
    if (this.contextMenu.isOpen()) {
      this.contextMenu.close();
      return;
    }
    if (this.newCollectionPopupOpen()) {
      this.closeNewCollectionPopup();
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
      this.newCollectionPopupOpen()
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
    this.state.selectView(new AllRequestsView());
  }

  /** Opens a smart view (Recent, Failing, Not used lately). */
  selectSmartView(view: SmartView): void {
    this.state.selectView(view);
  }

  selectSidebarItem(item: SidebarItem): void {
    this.selectItem(item);
  }

  // ---------- Add ----------

  /** "+": creates the item under a free default name and starts renaming it. */
  async createAndRename(): Promise<void> {
    const project = this.state.selectedProject();
    if (!project) return;
    this.contextMenu.close();
    try {
      let created: { kind: SidebarItem['kind']; id: number };
      switch (this.folderFilter()) {
        case 'collections': {
          const name = freeName('New collection', this.collections().map((c) => c.name));
          const collection = await this.collectionApi.create({ project_id: project.id, name });
          created = { kind: 'collection', id: collection.id };
          break;
        }
        case 'favourites': {
          const name = freeName('New favourites', this.favouriteCollections().map((f) => f.name));
          const favourite = await this.favouriteApi.createCollection({ project_id: project.id, name });
          created = { kind: 'favourite', id: favourite.id };
          break;
        }
        case 'scripts': {
          // Attaching an existing script's name returns that script, so the name must be free.
          const name = freeName('new script', this.allScripts().map((t) => t.name));
          const script = await this.scriptApi.attachScript(0, project.id, name);
          created = { kind: 'script', id: script.id };
          break;
        }
        default:
          return;
      }
      const item = this.filteredItems().find((i) => i.kind === created.kind && i.id === created.id);
      if (item) this.startRename(item);
    } catch (err) {
      console.error(err);
      this.toast.show(`Couldn't create it: ${errorText(err)}`);
    }
  }

  protected readonly canAddRequest = computed(() => !!this.state.selectedCollection() && !this.state.showingBin());

  /**
   * Opens a Collections header menu through the shared context menu, which is drawn at the
   * root of the app. A menu drawn inside the sidebar would be clipped and
   * positioned by it (the sidebar has a backdrop-filter and overflow: hidden).
   * It opens on the next tick so the click that asked for it, and the outlet's
   * document click listener, are finished first.
   */
  openCollectionsMenu(menu: TemplateRef<{ $implicit: null }>, position: { x: number; y: number }): void {
    setTimeout(() => this.contextMenu.open(menu, null, position.x, position.y), 0);
  }

  onCollectionsMenu(action: 'collection' | 'request' | 'curl' | 'import' | 'export'): void {
    this.contextMenu.close();
    switch (action) {
      case 'collection':
        void this.createAndRename();
        break;
      case 'request':
        this.state.addRequestIntent.set('blank');
        break;
      case 'curl':
        this.state.addRequestIntent.set('curl');
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
      const picks = await this.importPrompt.choose(rows);
      if (!picks || picks.length === 0) return;

      const result = await this.openapiApi.importSpec(project.id, path, picks);
      const parts: string[] = [];
      if (result.requests_created > 0) parts.push(`${result.requests_created} added`);
      if (result.requests_updated > 0) parts.push(`${result.requests_updated} updated`);
      this.toast.show(parts.length > 0 ? `Imported: ${parts.join(', ')}` : 'Nothing to import');
    } catch (err) {
      console.error(err);
      this.toast.show(`Import failed: ${errorText(err)}`);
    }
  }

  async exportOpenApi(): Promise<void> {
    const project = this.state.selectedProject();
    if (!project) return;

    const proceed = await this.confirm.ask({
      title: 'Export OpenAPI?',
      message:
        'The file lists your URLs and request bodies exactly as written, including any tokens or passwords in them. Header values are left out.',
      confirmLabel: 'Continue',
    });
    if (!proceed) return;

    const path = await Dialogs.SaveFile({
      Filename: 'openapi.json',
      Title: 'Export OpenAPI',
      ButtonText: 'Save',
    });
    if (typeof path !== 'string' || !path) return;

    try {
      const result = await this.openapiApi.exportSpec(project.id, path);
      const skipped =
        result.requests_skipped > 0 ? `, ${result.requests_skipped} skipped (same method and path)` : '';
      this.toast.show(
        `Exported ${result.requests_exported} requests across ${result.collections_exported} collections${skipped}`,
      );
    } catch (err) {
      console.error(err);
      this.toast.show(`Export failed: ${errorText(err)}`);
    }
  }

  closeNewCollectionPopup(): void {
    this.newCollectionPopupOpen.set(false);
    if (this.drag.createCollectionRequested()) {
      this.drag.cancelCreateCollection();
    }
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

  openAppearanceMenu(
    item: SidebarItem,
    event: MouseEvent | null,
    template: TemplateRef<{ $implicit: SidebarItem }>,
  ): void {
    let x: number;
    let y: number;
    if (event) {
      const rect = (event.currentTarget as HTMLElement).getBoundingClientRect();
      x = rect.left;
      y = rect.bottom + 4;
    } else {
      const at = this.contextMenu.position();
      x = at?.x ?? 0;
      y = at?.y ?? 0;
    }
    this.contextMenu.open(template, item, x, y);
  }

  async selectAppearance(item: SidebarItem, type: 'icon' | 'color', value: string): Promise<void> {
    await this.saveAppearance(item, { appearance_type: type, appearance_value: value });
  }

  async resetAppearance(item: SidebarItem): Promise<void> {
    await this.saveAppearance(item, { appearance_type: 'icon', appearance_value: this.iconManifest.defaultIconId() });
  }

  private async saveAppearance(item: SidebarItem, patch: SidebarAppearancePatch): Promise<void> {
    this.contextMenu.close();
    try {
      await this.applyAppearance(item, patch);
    } catch (err) {
      console.error(err);
    }
  }

  // ---------- Delete ----------

  async deleteItem(item: SidebarItem): Promise<void> {
    this.contextMenu.close();

    if (!(item.kind === 'collection' && (await this.isEmptyCollection(item.id)))) {
      const confirmed = await this.askToDelete(item);
      if (!confirmed) return;
    }

    try {
      await this.removeItem(item);
    } catch (err) {
      console.error(err);
    }
  }

  private async isEmptyCollection(id: number): Promise<boolean> {
    try {
      const requests = await this.commandApi.execute<unknown[] | null>('request.list_for_collection', { collection_id: id });
      return (requests ?? []).length === 0;
    } catch {
      return false;
    }
  }

  private askToDelete(item: SidebarItem): Promise<boolean> {
    const noun =
      item.kind === 'collection' ? 'collection' : item.kind === 'favourite' ? 'favourites' : 'script';
    return this.confirm.ask({
      title: `Delete ${noun}?`,
      message: this.deleteItemMessage(item),
      confirmLabel: 'Delete',
      danger: true,
    });
  }

  private deleteItemMessage(item: SidebarItem): string {
    switch (item.kind) {
      case 'collection':
        return `This will delete "${item.name}" and all requests inside it. This cannot be undone.`;
      case 'favourite':
        return `This will delete the favourites group "${item.name}". The requests themselves are not affected.`;
      case 'script':
        return `This will delete the script "${item.name}". The requests themselves are not affected.`;
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
        this.state.selectView(null);
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
        this.state.selectView(null);
        this.favouriteApi.requests.set([]);
      }
    } catch (err) {
      console.error(err);
    }
  }

  private async deleteScript(script: Script): Promise<void> {
    const project = this.state.selectedProject();
    if (!project) return;

    try {
      await this.scriptApi.deleteScript(project.id, script.name);
      if (this.state.selectedScript() === script.name) {
        this.state.selectView(null);
      }
    } catch (err) {
      console.error(err);
    }
  }

  // ---------- Helpers ----------

  isAppearanceSelected(appearance: SidebarItem['appearance'], type: 'icon' | 'color', value: string): boolean {
    return appearance.appearance_type === type && appearance.appearance_value === value;
  }

  appearanceIconEntries(item: SidebarItem) {
    return this.iconManifest.packIcons(FOLDER_FOR_KIND[item.kind]);
  }
}

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/** base, or "base 2", "base 3"… when names (compared case-insensitively) already use it. */
function freeName(base: string, names: string[]): string {
  const taken = new Set(names.map((n) => n.toLowerCase()));
  let name = base;
  for (let n = 2; taken.has(name.toLowerCase()); n++) name = `${base} ${n}`;
  return name;
}
