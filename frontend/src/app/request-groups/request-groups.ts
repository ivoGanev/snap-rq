import { Component, computed, effect, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { WorkspaceStateService } from '../core/services/workspace-state.service';
import { CollectionApiService, type Collection } from '../core/services/collection.service';
import { FavouriteApiService, type FavouriteCollection } from '../core/services/favourite.service';
import { RequestApiService } from '../core/services/request.service';
import { SelectionStateService } from '../core/services/selection-state.service';
import { IconManifestService } from '../core/services/icon-manifest.service';
import { TagApiService, type Tag } from '../core/services/tag.service';

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

@Component({
  selector: 'app-request-groups',
  imports: [FormsModule],
  templateUrl: './request-groups.html',
  styleUrl: './request-groups.scss',
  host: {
    class: 'sidebar sidebar-left',
    'aria-label': 'Collections and Favourites',
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

  readonly collectionsExpanded = signal(true);
  readonly favouritesExpanded = signal(false);
  readonly tagsExpanded = signal(true);
  readonly tagSearchQuery = signal('');
  readonly collectionContextMenuOpen = signal(false);
  readonly collectionContextMenuX = signal(0);
  readonly collectionContextMenuY = signal(0);
  readonly collectionContextMenuTarget = signal<Collection | null>(null);
  readonly collectionAppearancePopupOpen = signal(false);
  readonly collectionAppearanceTarget = signal<Collection | null>(null);
  readonly collectionAppearanceTab = signal<'icon' | 'color'>('icon');
  readonly collectionColorPalette = COLLECTION_COLOR_PALETTE;
  readonly newCollectionPopupOpen = signal(false);
  readonly newCollectionName = signal('');

  readonly filteredTags = computed<Tag[]>(() => {
    const query = this.tagSearchQuery().trim().toLowerCase();
    const tags = this.allTags();
    if (!query) return tags;
    return tags.filter(tag => tag.name.toLowerCase().includes(query));
  });

  constructor() {
    effect(() => {
      if (this.state.selectedCollection()) {
        this.collectionsExpanded.set(true);
      }
    });
  }

  onEscapePressed(): void {
    if (this.collectionContextMenuOpen()) {
      this.closeCollectionContextMenu();
      return;
    }
    if (this.collectionAppearancePopupOpen()) {
      this.closeCollectionAppearancePopup();
      return;
    }
    if (this.newCollectionPopupOpen()) {
      this.closeNewCollectionPopup();
      return;
    }
  }

  toggleCollectionsExpanded(): void {
    this.collectionsExpanded.update(v => !v);
  }

  toggleFavouritesExpanded(): void {
    this.favouritesExpanded.update(v => !v);
  }

  toggleTagsExpanded(): void {
    this.tagsExpanded.update(v => !v);
  }

  selectCollection(collection: Collection): void {
    this.state.selectedCollection.set(collection);
    this.state.selectedFavouriteCollection.set(null);
    this.state.selectedTag.set(null);
    this.collectionsExpanded.set(true);
  }

  selectFavouriteCollection(collection: FavouriteCollection): void {
    this.state.selectedFavouriteCollection.set(collection);
    this.state.selectedCollection.set(null);
    this.state.selectedTag.set(null);
    this.favouritesExpanded.set(true);
  }

  selectTag(tag: Tag): void {
    this.state.selectedTag.set(tag.name);
    this.state.selectedCollection.set(null);
    this.state.selectedFavouriteCollection.set(null);
    this.tagsExpanded.set(true);
  }

  async deleteTag(tag: Tag, event: MouseEvent): Promise<void> {
    event.stopPropagation();
    try {
      await this.tagApi.deleteTag(tag.name);
      if (this.state.selectedTag() === tag.name) {
        this.state.selectedTag.set(null);
      }
    } catch (err) {
      console.error(err);
    }
  }

  openCollectionContextMenu(collection: Collection, event: MouseEvent): void {
    event.preventDefault();
    this.collectionContextMenuTarget.set(collection);
    this.collectionContextMenuX.set(event.clientX);
    this.collectionContextMenuY.set(event.clientY);
    this.collectionContextMenuOpen.set(true);
  }

  closeCollectionContextMenu(): void {
    this.collectionContextMenuOpen.set(false);
    this.collectionContextMenuTarget.set(null);
  }

  editCollectionAppearance(): void {
    const collection = this.collectionContextMenuTarget();
    if (!collection) return;

    this.closeCollectionContextMenu();
    this.openCollectionAppearancePopup(collection);
  }

  openCollectionAppearancePopup(collection: Collection, event?: MouseEvent): void {
    event?.preventDefault();
    event?.stopPropagation();
    this.collectionAppearanceTarget.set(collection);
    this.collectionAppearanceTab.set('icon');
    this.collectionAppearancePopupOpen.set(true);
  }

  closeCollectionAppearancePopup(): void {
    this.collectionAppearancePopupOpen.set(false);
    this.collectionAppearanceTarget.set(null);
  }

  async selectCollectionAppearanceColor(color: string): Promise<void> {
    const collection = this.collectionAppearanceTarget();
    if (!collection) return;

    try {
      const updated = await this.collectionApi.updateAppearance(collection.id, {
        appearance_type: 'color',
        appearance_value: color,
      });
      this.collectionAppearanceTarget.set({ ...collection, appearance: updated });
    } catch (err) {
      console.error(err);
    }
  }

  onAppearanceColorInput(event: Event): void {
    const value = (event.target as HTMLInputElement).value;
    this.selectCollectionAppearanceColor(value);
  }

  async selectCollectionAppearanceIcon(iconId: string): Promise<void> {
    const collection = this.collectionAppearanceTarget();
    if (!collection) return;

    try {
      const updated = await this.collectionApi.updateAppearance(collection.id, {
        appearance_type: 'icon',
        appearance_value: iconId,
      });
      this.collectionAppearanceTarget.set({ ...collection, appearance: updated });
    } catch (err) {
      console.error(err);
    }
  }

  async resetCollectionAppearance(): Promise<void> {
    const collection = this.collectionAppearanceTarget();
    if (!collection) return;

    try {
      const updated = await this.collectionApi.updateAppearance(collection.id, {
        appearance_type: 'icon',
        appearance_value: 'default',
      });
      this.collectionAppearanceTarget.set({ ...collection, appearance: updated });
    } catch (err) {
      console.error(err);
    }
  }

  async deleteCollection(): Promise<void> {
    const collection = this.collectionContextMenuTarget();
    if (!collection) return;

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
        this.state.rightPanelMode.set('response');
      }

      this.closeCollectionContextMenu();
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }

  openNewCollectionPopup(): void {
    this.newCollectionName.set('');
    this.newCollectionPopupOpen.set(true);
  }

  closeNewCollectionPopup(): void {
    this.newCollectionPopupOpen.set(false);
  }

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
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }
}
