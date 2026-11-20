import { ChangeDetectionStrategy, Component, inject, input, model, output } from '@angular/core';
import { SelectOnFocusDirective } from '../core/directives/select-on-focus.directive';
import type { Collection, CollectionAppearance } from '../core/services/collection.service';
import type { FavouriteCollection, FavouriteAppearance } from '../core/services/favourite.service';
import type { Script, ScriptAppearance } from '../core/services/script.service';
import { IconManifestService } from '../core/services/icon-manifest.service';

export type SidebarFolder = 'collections' | 'favourites' | 'scripts';
export type SidebarItemAppearance = CollectionAppearance | FavouriteAppearance | ScriptAppearance;

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

export interface ScriptSidebarItem {
  kind: 'script';
  id: number;
  name: string;
  appearance: ScriptAppearance;
  data: Script;
}

export type SidebarItem = CollectionSidebarItem | FavouriteSidebarItem | ScriptSidebarItem;

export const FOLDER_FOR_KIND: Record<SidebarItem['kind'], SidebarFolder> = {
  collection: 'collections',
  favourite: 'favourites',
  script: 'scripts',
};

/** Stable identity of a sidebar item, e.g. `collection:12` or `script:auth`. */
export const sidebarCollectionKey = (id: number): string => `collection:${id}`;
export const sidebarFavouriteKey = (id: number): string => `favourite:${id}`;
export const sidebarScriptKey = (name: string): string => `script:${name}`;

export function sidebarItemKey(item: SidebarItem): string {
  return item.kind === 'script'
    ? sidebarScriptKey(item.name)
    : item.kind === 'favourite'
      ? sidebarFavouriteKey(item.id)
      : sidebarCollectionKey(item.id);
}

export interface SidebarAppearancePatch {
  appearance_type: string;
  appearance_value: string;
}

export interface AddMenuPosition {
  x: number;
  y: number;
}

const FOLDER_META: Record<SidebarFolder, { title: string; emptyMessage: string }> = {
  collections: { title: 'Collections', emptyMessage: 'No collections.' },
  favourites: { title: 'Favourites', emptyMessage: 'No favourite collections.' },
  scripts: { title: 'Scripts', emptyMessage: 'No scripts yet.' },
};

@Component({
  selector: 'app-request-group-list',
  imports: [SelectOnFocusDirective],
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
  readonly dropTargetKey = input<string | null>(null);
  readonly dropZoneActive = input<boolean>(false);
  readonly showAddMenu = input<boolean>(false);

  readonly addClick = output<void>();
  readonly addMenuOpen = output<AddMenuPosition>();
  readonly itemSelect = output<SidebarItem>();
  readonly itemContextMenu = output<{ item: SidebarItem; event: MouseEvent }>();
  readonly appearanceClick = output<{ item: SidebarItem; event: MouseEvent }>();
  readonly renameStart = output<SidebarItem>();
  readonly renameSubmit = output<void>();
  readonly renameCancel = output<void>();

  isSelected(item: SidebarItem): boolean {
    return this.selectedKey() === sidebarItemKey(item);
  }

  isDropTarget(item: SidebarItem): boolean {
    return this.dropTargetKey() === sidebarItemKey(item);
  }

  folderTitle(): string {
    return FOLDER_META[this.folderFilter()].title;
  }

  activeFolderEmptyMessage(): string {
    return FOLDER_META[this.folderFilter()].emptyMessage;
  }

  dropCreateMessage(): string {
    return 'Drop to create a collection';
  }

  onAddClick(): void {
    this.addClick.emit();
  }

  onAddMenuClick(event: MouseEvent): void {
    // Keep the click from reaching the context-menu outlet's document click
    // listener, which closes any open menu.
    event.preventDefault();
    event.stopPropagation();
    const rect = (event.currentTarget as HTMLElement).getBoundingClientRect();
    this.addMenuOpen.emit({ x: rect.left, y: rect.bottom + 4 });
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

  iconPath(item: SidebarItem): string | undefined {
    const appearance = item.appearance;
    if (appearance.appearance_type !== 'icon') return undefined;
    return this.iconManifest.iconPath(FOLDER_FOR_KIND[item.kind], appearance.appearance_value);
  }

  colorValue(appearance: SidebarItemAppearance): string | null {
    return appearance.appearance_type === 'color' ? appearance.appearance_value : null;
  }

  isDefaultAppearance(appearance: SidebarItemAppearance): boolean {
    return (
      appearance.appearance_type === 'icon' &&
      appearance.appearance_value === this.iconManifest.defaultIconId()
    );
  }
}
