import { ChangeDetectionStrategy, Component, inject, input, model, output } from '@angular/core';
import { FormsModule } from '@angular/forms';
import type { Collection, CollectionAppearance } from '../core/services/collection.service';
import type { FavouriteCollection, FavouriteAppearance } from '../core/services/favourite.service';
import type { Tag, TagAppearance } from '../core/services/tag.service';
import { IconManifestService } from '../core/services/icon-manifest.service';

export type SidebarFolder = 'collections' | 'favourites' | 'tags';
export type SidebarItemAppearance = CollectionAppearance | FavouriteAppearance | TagAppearance;

/** Discriminated union of sidebar entries; `kind` narrows `data` and `appearance`. */
export interface CollectionSidebarItem {
  kind: 'collection';
  id: number;
  name: string;
  appearance: CollectionAppearance;
  data: Collection;
}

export interface FavouriteSidebarItem {
  kind: 'favourite';
  id: number;
  name: string;
  appearance: FavouriteAppearance;
  data: FavouriteCollection;
}

export interface TagSidebarItem {
  kind: 'tag';
  id: number;
  name: string;
  appearance: TagAppearance;
  data: Tag;
}

export type SidebarItem = CollectionSidebarItem | FavouriteSidebarItem | TagSidebarItem;

export const FOLDER_FOR_KIND: Record<SidebarItem['kind'], SidebarFolder> = {
  collection: 'collections',
  favourite: 'favourites',
  tag: 'tags',
};

/** Stable identity of a sidebar item, e.g. `collection:12` or `tag:auth`. */
export const sidebarCollectionKey = (id: number): string => `collection:${id}`;
export const sidebarFavouriteKey = (id: number): string => `favourite:${id}`;
export const sidebarTagKey = (name: string): string => `tag:${name}`;

export function sidebarItemKey(item: SidebarItem): string {
  return item.kind === 'tag'
    ? sidebarTagKey(item.name)
    : item.kind === 'favourite'
      ? sidebarFavouriteKey(item.id)
      : sidebarCollectionKey(item.id);
}

export interface SidebarAppearancePatch {
  appearance_type: string;
  appearance_value: string;
}

const FOLDER_META: Record<SidebarFolder, { title: string; emptyMessage: string }> = {
  collections: { title: 'Collections', emptyMessage: 'No collections.' },
  favourites: { title: 'Favourites', emptyMessage: 'No favourite collections.' },
  tags: { title: 'Tags', emptyMessage: 'No tags yet.' },
};

@Component({
  selector: 'app-request-group-list',
  imports: [FormsModule],
  templateUrl: './request-group-list.html',
  styleUrl: './request-group-list.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class RequestGroupList {
  private readonly iconManifest = inject(IconManifestService);

  readonly folderFilter = input.required<SidebarFolder>();
  readonly items = input.required<SidebarItem[]>();
  readonly renamingItem = input<SidebarItem | null>(null);
  readonly renameValue = model('');
  readonly selectedKey = input<string | null>(null);

  readonly addClick = output<void>();
  readonly itemSelect = output<SidebarItem>();
  readonly itemContextMenu = output<{ item: SidebarItem; event: MouseEvent }>();
  readonly appearanceClick = output<{ item: SidebarItem; event: MouseEvent }>();
  readonly renameStart = output<SidebarItem>();
  readonly renameSubmit = output<void>();
  readonly renameCancel = output<void>();

  isSelected(item: SidebarItem): boolean {
    return this.selectedKey() === sidebarItemKey(item);
  }

  folderTitle(): string {
    return FOLDER_META[this.folderFilter()].title;
  }

  activeFolderEmptyMessage(): string {
    return FOLDER_META[this.folderFilter()].emptyMessage;
  }

  onAddClick(): void {
    this.addClick.emit();
  }

  onItemClick(item: SidebarItem): void {
    this.itemSelect.emit(item);
  }

  onContextMenu(item: SidebarItem, event: MouseEvent): void {
    event.preventDefault();
    event.stopPropagation();
    this.itemContextMenu.emit({ item, event });
  }

  onAppearanceClick(item: SidebarItem, event: MouseEvent): void {
    event.stopPropagation();
    this.appearanceClick.emit({ item, event });
  }

  onRenameStart(item: SidebarItem): void {
    this.renameStart.emit(item);
  }

  onRenameInput(value: string): void {
    this.renameValue.set(value);
  }

  onRenameSubmit(): void {
    this.renameSubmit.emit();
  }

  onRenameCancel(): void {
    this.renameCancel.emit();
  }

  iconPath(appearance: SidebarItemAppearance): string | undefined {
    if (appearance.appearance_type !== 'icon') return undefined;
    return this.iconManifest.pathFor(appearance.appearance_value);
  }

  colorValue(appearance: SidebarItemAppearance): string | null {
    return appearance.appearance_type === 'color' ? appearance.appearance_value : null;
  }

  isDefaultAppearance(appearance: SidebarItemAppearance): boolean {
    return appearance.appearance_type === 'icon' && appearance.appearance_value === 'default';
  }
}
