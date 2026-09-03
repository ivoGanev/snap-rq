import { Injectable, signal } from '@angular/core';

export interface CollectionColor {
  color?: string;
}

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

@Injectable({ providedIn: 'root' })
export class CollectionAppearanceService {
  readonly colorMap = signal<Record<number, CollectionColor>>({});
  readonly palette = COLLECTION_PALETTE;

  colorFor(collectionId: number): CollectionColor {
    return this.colorMap()[collectionId] ?? {};
  }

  setColor(collectionId: number, color: string): void {
    this.colorMap.update(map => ({
      ...map,
      [collectionId]: { color },
    }));
  }

  clearColor(collectionId: number): void {
    this.colorMap.update(map => {
      const { [collectionId]: _, ...rest } = map;
      return rest;
    });
  }
}
