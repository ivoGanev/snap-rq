import { Component, computed, effect, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { WorkspaceStateService } from '../core/services/workspace-state.service';
import { CollectionApiService, type Collection, type CollectionAppearance } from '../core/services/collection.service';
import { FavouriteApiService, type FavouriteCollection, type FavouriteAppearance } from '../core/services/favourite.service';
import { RequestApiService } from '../core/services/request.service';
import { SelectionStateService } from '../core/services/selection-state.service';
import { IconManifestService } from '../core/services/icon-manifest.service';
import { TagApiService, type Tag, type TagAppearance } from '../core/services/tag.service';

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

type SidebarFolder = 'collections' | 'favourites' | 'tags';
type SidebarItemType = 'collection' | 'favourite' | 'tag';
type Appearance = CollectionAppearance | FavouriteAppearance | TagAppearance;

interface SidebarItem {
  id: number;
  name: string;
  type: SidebarItemType;
  folder: SidebarFolder;
  appearance: Appearance;
  data: Collection | FavouriteCollection | Tag;
}

/**
 * Unified sidebar for Collections, Favourites, and Tags rendered as a single
 * folder tree with shared expand/collapse, search, + menu, and context menus.
 */
@Component({
  selector: 'app-request-groups',
  imports: [FormsModule],
  templateUrl: './request-groups.html',
  styleUrl: './request-groups.scss',
  host: {
    class: 'sidebar sidebar-left',
    'aria-label': 'Collections, Favourites, and Tags',
    '(window:keydown.escape)': 'onEscapePressed()',
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

  protected readonly collections = this.collectionApi.collections;
  protected readonly favouriteCollections = this.favouriteApi.collections;
  protected readonly allTags = this.tagApi.allTags;
  protected readonly loading = this.state.loading;

  protected readonly searchQuery = signal('');
  protected readonly addMenuOpen = signal(false);

  // Expansion state manually controlled by the user; restored when search is cleared.
  protected readonly userExpanded = signal<Set<SidebarFolder>>(new Set(['collections', 'tags']));
  private expandedBeforeSearch: Set<SidebarFolder> | null = null;

  // Creation popups
  protected readonly newCollectionPopupOpen = signal(false);
  protected readonly newCollectionName = signal('');
  protected readonly newFavouritePopupOpen = signal(false);
  protected readonly newFavouriteName = signal('');
  protected readonly newTagPopupOpen = signal(false);
  protected readonly newTagName = signal('');

  // Context menu
  protected readonly contextMenuOpen = signal(false);
  protected readonly contextMenuX = signal(0);
  protected readonly contextMenuY = signal(0);
  protected readonly contextMenuItem = signal<SidebarItem | null>(null);
  protected readonly contextMenuFolder = signal<SidebarFolder | null>(null);

  // Rename inline
  protected readonly renamingItem = signal<SidebarItem | null>(null);
  protected readonly renameValue = signal('');

  // Appearance popup
  protected readonly appearancePopupOpen = signal(false);
  protected readonly appearanceTarget = signal<SidebarItem | null>(null);
  protected readonly appearanceTab = signal<'icon' | 'color'>('icon');
  protected readonly collectionColorPalette = COLLECTION_COLOR_PALETTE;

  readonly filteredItems = computed<SidebarItem[]>(() => {
    const query = this.searchQuery().trim().toLowerCase();
    const items: SidebarItem[] = [
      ...this.collections().map((c) => this.toSidebarItem(c, 'collection')),
      ...this.favouriteCollections().map((f) => this.toSidebarItem(f, 'favourite')),
      ...this.allTags().map((t) => this.toSidebarItem(t, 'tag')),
    ];
    if (!query) return items;
    return items.filter((item) => item.name.toLowerCase().includes(query));
  });

  readonly filteredCollections = computed(() =>
    this.filteredItems().filter((i) => i.folder === 'collections'),
  );
  readonly filteredFavourites = computed(() =>
    this.filteredItems().filter((i) => i.folder === 'favourites'),
  );
  readonly filteredTags = computed(() => this.filteredItems().filter((i) => i.folder === 'tags'));

  readonly expandedFolders = computed<Set<SidebarFolder>>(() => {
    const query = this.searchQuery().trim();
    if (!query) {
      return this.userExpanded();
    }
    // While searching, expand any folder that has matches.
    const expanded = new Set<SidebarFolder>();
    for (const item of this.filteredItems()) {
      expanded.add(item.folder);
    }
    return expanded;
  });

  constructor() {
    effect(() => {
      const query = this.searchQuery().trim();
      if (query) {
        if (this.expandedBeforeSearch === null) {
          this.expandedBeforeSearch = new Set(this.userExpanded());
        }
      } else {
        if (this.expandedBeforeSearch !== null) {
          this.userExpanded.set(this.expandedBeforeSearch);
          this.expandedBeforeSearch = null;
        }
      }
    });
  }

  private toSidebarItem(
    data: Collection | FavouriteCollection | Tag,
    type: SidebarItemType,
  ): SidebarItem {
    return {
      id: data.id,
      name: data.name,
      type,
      folder: type === 'collection' ? 'collections' : type === 'favourite' ? 'favourites' : 'tags',
      appearance: data.appearance,
      data,
    };
  }

  onEscapePressed(): void {
    if (this.contextMenuOpen()) {
      this.closeContextMenu();
      return;
    }
    if (this.appearancePopupOpen()) {
      this.closeAppearancePopup();
      return;
    }
    if (this.addMenuOpen()) {
      this.closeAddMenu();
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

  // ---------- Folder expansion ----------

  isFolderExpanded(folder: SidebarFolder): boolean {
    return this.expandedFolders().has(folder);
  }

  toggleFolder(folder: SidebarFolder, event?: MouseEvent): void {
    event?.stopPropagation();
    const current = new Set(this.userExpanded());
    if (current.has(folder)) {
      current.delete(folder);
    } else {
      current.add(folder);
    }
    this.userExpanded.set(current);
  }

  // ---------- Selection ----------

  selectCollection(data: Collection | FavouriteCollection | Tag): void {
    const collection = data as Collection;
    this.state.selectedCollection.set(collection);
    this.state.selectedFavouriteCollection.set(null);
    this.state.selectedTag.set(null);
    this.expandFolder('collections');
  }

  selectFavouriteCollection(data: Collection | FavouriteCollection | Tag): void {
    const collection = data as FavouriteCollection;
    this.state.selectedFavouriteCollection.set(collection);
    this.state.selectedCollection.set(null);
    this.state.selectedTag.set(null);
    this.expandFolder('favourites');
  }

  selectTag(data: Collection | FavouriteCollection | Tag): void {
    const tag = data as Tag;
    this.state.selectedTag.set(tag.name);
    this.state.selectedCollection.set(null);
    this.state.selectedFavouriteCollection.set(null);
    this.expandFolder('tags');
  }

  private expandFolder(folder: SidebarFolder): void {
    if (this.searchQuery().trim()) return;
    this.userExpanded.update((set) => new Set([...set, folder]));
  }

  // ---------- Add menu ----------

  openAddMenu(): void {
    this.addMenuOpen.set(true);
  }

  closeAddMenu(): void {
    this.addMenuOpen.set(false);
  }

  openNewCollectionPopup(): void {
    this.closeAddMenu();
    this.newCollectionName.set('');
    this.newCollectionPopupOpen.set(true);
  }

  closeNewCollectionPopup(): void {
    this.newCollectionPopupOpen.set(false);
  }

  openNewFavouritePopup(): void {
    this.closeAddMenu();
    this.newFavouriteName.set('');
    this.newFavouritePopupOpen.set(true);
  }

  closeNewFavouritePopup(): void {
    this.newFavouritePopupOpen.set(false);
  }

  openNewTagPopup(): void {
    this.closeAddMenu();
    this.newTagName.set('');
    this.newTagPopupOpen.set(true);
  }

  closeNewTagPopup(): void {
    this.newTagPopupOpen.set(false);
  }

  // ---------- Creation ----------

  async addCollection(): Promise<void> {
    const project = this.state.selectedProject();
    if (!project) return;

    const name = this.newCollectionName().trim();
    if (!name) return;

    this.state.loading.set(true);
    try {
      await this.collectionApi.create({
        project_id: project.id,
        name,
      });
      await this.collectionApi.loadForProject(project.id);
      this.closeNewCollectionPopup();
      this.expandFolder('collections');
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }

  async addFavouriteCollection(): Promise<void> {
    const profile = this.state.selectedProject();
    if (!profile) return;

    const name = this.newFavouriteName().trim();
    if (!name) return;

    try {
      await this.favouriteApi.createCollection({ profile_id: profile.profile_id, name });
      this.closeNewFavouritePopup();
      this.expandFolder('favourites');
    } catch (err) {
      console.error(err);
    }
  }

  async addTag(): Promise<void> {
    const name = this.newTagName().trim().toLowerCase();
    if (!name) return;

    try {
      await this.tagApi.addTagToRequest(0, name);
      this.closeNewTagPopup();
      this.expandFolder('tags');
    } catch (err) {
      console.error(err);
    }
  }

  // ---------- Context menu ----------

  openItemContextMenu(item: SidebarItem, event: MouseEvent): void {
    event.preventDefault();
    event.stopPropagation();
    this.contextMenuFolder.set(null);
    this.contextMenuItem.set(item);
    this.contextMenuX.set(event.clientX);
    this.contextMenuY.set(event.clientY);
    this.contextMenuOpen.set(true);
  }

  openFolderContextMenu(folder: SidebarFolder, event: MouseEvent): void {
    event.preventDefault();
    event.stopPropagation();
    this.contextMenuItem.set(null);
    this.contextMenuFolder.set(folder);
    this.contextMenuX.set(event.clientX);
    this.contextMenuY.set(event.clientY);
    this.contextMenuOpen.set(true);
  }

  closeContextMenu(): void {
    this.contextMenuOpen.set(false);
    this.contextMenuItem.set(null);
    this.contextMenuFolder.set(null);
  }

  // ---------- Rename ----------

  startRename(): void {
    const item = this.contextMenuItem();
    if (!item) return;
    this.closeContextMenu();
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
      if (item.type === 'collection') {
        const collection = item.data as Collection;
        await this.collectionApi.update({ ...collection, name: newName });
      } else if (item.type === 'favourite') {
        const favourite = item.data as FavouriteCollection;
        await this.favouriteApi.updateCollection({ ...favourite, name: newName });
      } else {
        const tag = item.data as Tag;
        await this.tagApi.renameTag(tag.name, newName);
        if (this.state.selectedTag() === tag.name) {
          this.state.selectedTag.set(newName);
        }
      }
    } catch (err) {
      console.error(err);
    } finally {
      this.cancelRename();
    }
  }

  // ---------- Appearance ----------

  openAppearancePopup(item: SidebarItem, event?: MouseEvent): void {
    event?.stopPropagation();
    this.closeContextMenu();
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
  }

  async selectAppearanceColor(color: string): Promise<void> {
    await this.saveAppearance({ appearance_type: 'color', appearance_value: color });
  }

  async resetAppearance(): Promise<void> {
    await this.saveAppearance({ appearance_type: 'icon', appearance_value: 'default' });
  }

  private async saveAppearance(
    appearance: Omit<Appearance, 'id' | 'collection_id' | 'favourite_collection_id' | 'tag_id'>,
  ): Promise<void> {
    const item = this.appearanceTarget();
    if (!item) return;

    try {
      if (item.type === 'collection') {
        await this.collectionApi.updateAppearance(item.id, appearance as Omit<CollectionAppearance, 'id' | 'collection_id'>);
      } else if (item.type === 'favourite') {
        await this.favouriteApi.updateAppearance(
          item.id,
          appearance as Omit<FavouriteAppearance, 'id' | 'favourite_collection_id'>,
        );
      } else {
        await this.tagApi.updateAppearance(item.id, appearance as Omit<TagAppearance, 'id' | 'tag_id'>);
      }
    } catch (err) {
      console.error(err);
    }
  }

  onAppearanceColorInput(event: Event): void {
    const value = (event.target as HTMLInputElement).value;
    void this.selectAppearanceColor(value);
  }

  // ---------- Delete ----------

  async deleteItem(): Promise<void> {
    const item = this.contextMenuItem();
    if (!item) return;
    this.closeContextMenu();

    try {
      if (item.type === 'collection') {
        await this.deleteCollection(item.data as Collection);
      } else if (item.type === 'favourite') {
        await this.deleteFavouriteCollection(item.data as FavouriteCollection);
      } else {
        await this.deleteTag(item.data as Tag);
      }
    } catch (err) {
      console.error(err);
    }
  }

  private async deleteCollection(collection: Collection): Promise<void> {
    this.state.loading.set(true);
    try {
      const deletedRequestIds = await this.collectionApi.delete(collection.id);
      for (const requestId of deletedRequestIds) {
        this.selectionState.deleteRequest(requestId);
      }
      this.selectionState.setSelectedRequestForCollection(collection.id, null);

      if (this.state.selectedCollection()?.id === collection.id) {
        this.state.selectedCollection.set(null);
        this.state.selectedRequest.set(null);
        this.state.selectedResponse.set(null);
        this.requestApi.requests.set([]);
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
    try {
      await this.tagApi.deleteTag(tag.name);
      if (this.state.selectedTag() === tag.name) {
        this.state.selectedTag.set(null);
      }
    } catch (err) {
      console.error(err);
    }
  }

  // ---------- Folder-level actions ----------

  folderActionCreate(folder: SidebarFolder): void {
    this.closeContextMenu();
    if (folder === 'collections') this.openNewCollectionPopup();
    else if (folder === 'favourites') this.openNewFavouritePopup();
    else this.openNewTagPopup();
  }

  folderActionCollapse(folder: SidebarFolder): void {
    this.closeContextMenu();
    this.userExpanded.update((set) => {
      const next = new Set(set);
      next.delete(folder);
      return next;
    });
  }

  folderActionExpand(folder: SidebarFolder): void {
    this.closeContextMenu();
    this.userExpanded.update((set) => new Set([...set, folder]));
  }

  // ---------- Helpers ----------

  isSelected(item: SidebarItem): boolean {
    if (item.type === 'collection') {
      return this.state.selectedCollection()?.id === item.id;
    }
    if (item.type === 'favourite') {
      return this.state.selectedFavouriteCollection()?.id === item.id;
    }
    return this.state.selectedTag() === item.name;
  }

  iconPath(appearance: Appearance): string | undefined {
    if (appearance.appearance_type !== 'icon') return undefined;
    return this.iconManifest.pathFor(appearance.appearance_value);
  }

  colorValue(appearance: Appearance): string | null {
    return appearance.appearance_type === 'color' ? appearance.appearance_value : null;
  }

  isDefaultAppearance(appearance: Appearance): boolean {
    return appearance.appearance_type === 'icon' && appearance.appearance_value === 'default';
  }

  isAppearanceSelected(appearance: Appearance, type: 'icon' | 'color', value: string): boolean {
    return appearance.appearance_type === type && appearance.appearance_value === value;
  }
}
