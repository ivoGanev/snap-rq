import { Injectable, signal } from '@angular/core';

export interface CollectionAppearance {
  color?: string;
  icon?: string;
}

export const DEFAULT_COLLECTION_COLOR = '#6b7280';

export const COLLECTION_PALETTE: string[] = [
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

export const COLLECTION_ICONS: { name: string; src: string }[] = [
  { name: 'Parchment', src: '/icons/parchment.png' },
  { name: 'Heart', src: '/icons/heart.png' },
  { name: 'Dollars', src: '/icons/dollars.png' },
  { name: 'Diamond', src: '/icons/diamond.png' },
];

@Injectable({ providedIn: 'root' })
export class CollectionAppearanceService {
  readonly appearanceMap = signal<Record<number, CollectionAppearance>>({});
  readonly palette = COLLECTION_PALETTE;
  readonly icons = COLLECTION_ICONS;

  appearanceFor(collectionId: number): CollectionAppearance {
    return this.appearanceMap()[collectionId] ?? {};
  }

  setColor(collectionId: number, color: string): void {
    this.appearanceMap.update(map => ({
      ...map,
      [collectionId]: { color, icon: undefined },
    }));
  }

  setIcon(collectionId: number, icon: string): void {
    this.appearanceMap.update(map => ({
      ...map,
      [collectionId]: { icon, color: undefined },
    }));
  }

  clear(collectionId: number): void {
    this.appearanceMap.update(map => {
      const { [collectionId]: _, ...rest } = map;
      return rest;
    });
  }
}
