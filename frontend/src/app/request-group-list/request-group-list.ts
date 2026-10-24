import { ChangeDetectionStrategy, Component, inject, input, model, output } from '@angular/core';
import { FormsModule } from '@angular/forms';
import type { Collection, CollectionAppearance } from '../core/services/collection.service';
import type { FavouriteCollection, FavouriteAppearance } from '../core/services/favourite.service';
import type { Tag, TagAppearance } from '../core/services/tag.service';
import { IconManifestService } from '../core/services/icon-manifest.service';

export type SidebarFolder = 'collections' | 'favourites' | 'tags';
export type SidebarItemType = 'collection' | 'favourite' | 'tag';
export type SidebarItemAppearance = CollectionAppearance | FavouriteAppearance | TagAppearance;

export interface SidebarItem {
  id: number;
  name: string;
  type: SidebarItemType;
  folder: SidebarFolder;
  appearance: SidebarItemAppearance;
  data: Collection | FavouriteCollection | Tag;
}

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
  readonly selectedCollectionId = input<number | null>(null);
  readonly selectedFavouriteId = input<number | null>(null);
  readonly selectedTagName = input<string | null>(null);

  readonly addClick = output<void>();
  readonly itemSelect = output<SidebarItem>();
  readonly itemContextMenu = output<{ item: SidebarItem; event: MouseEvent }>();
  readonly appearanceClick = output<{ item: SidebarItem; event: MouseEvent }>();
  readonly renameStart = output<SidebarItem>();
  readonly renameSubmit = output<void>();
  readonly renameCancel = output<void>();

  isSelected(item: SidebarItem): boolean {
    if (item.type === 'collection') {
      return this.selectedCollectionId() === item.id;
    }
    if (item.type === 'favourite') {
      return this.selectedFavouriteId() === item.id;
    }
    return this.selectedTagName() === item.name;
  }

  activeFolderEmptyMessage(): string {
    switch (this.folderFilter()) {
      case 'collections':
        return 'No collections.';
      case 'favourites':
        return 'No favourite collections.';
      case 'tags':
        return 'No tags yet.';
      default:
        return '';
    }
  }

  folderTitle(): string {
    switch (this.folderFilter()) {
      case 'collections':
        return 'Collections';
      case 'favourites':
        return 'Favourites';
      case 'tags':
        return 'Tags';
      default:
        return '';
    }
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
