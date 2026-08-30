import { Injectable, signal } from '@angular/core';
import * as CollectionService from '../../../../bindings/snap-rq/backend/services';
import type { Collection } from '../../../../bindings/snap-rq/backend/models';

export type { Collection };

@Injectable({ providedIn: 'root' })
export class CollectionApiService {
  readonly collections = signal<Collection[]>([]);

  async loadAll(): Promise<void> {
    const all = await CollectionService.CollectionService.GetAllCollections();
    this.collections.set(all ?? []);
  }

  async loadForProject(projectId: number): Promise<void> {
    const all = await CollectionService.CollectionService.GetCollectionsForProject(projectId);
    this.collections.set(all ?? []);
  }
}
