import { Injectable, signal } from '@angular/core';
import * as CollectionService from '../../../../bindings/snap-rq/backend/services';
import type { Collection, CollectionAppearance } from '../../../../bindings/snap-rq/backend/models';

export type { Collection, CollectionAppearance };

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

  async create(collection: Omit<Collection, 'id' | 'appearance'>): Promise<Collection> {
    const created = await CollectionService.CollectionService.CreateCollection(collection as Collection);
    await this.loadAll();
    return created;
  }

  async update(collection: Collection): Promise<Collection> {
    const updated = await CollectionService.CollectionService.UpdateCollection(collection);
    this.collections.update(list =>
      list.map(c => (c.id === updated.id ? updated : c)),
    );
    return updated;
  }

  async updateAppearance(
    collectionId: number,
    appearance: Omit<CollectionAppearance, 'id' | 'collection_id'>,
  ): Promise<CollectionAppearance> {
    const updated = await CollectionService.CollectionService.UpdateCollectionAppearance(
      collectionId,
      { ...appearance, id: 0, collection_id: collectionId } as CollectionAppearance,
    );
    this.collections.update(list =>
      list.map(c => (c.id === collectionId ? { ...c, appearance: updated } : c)),
    );
    return updated;
  }

  async delete(id: number): Promise<number[]> {
    const deletedRequestIds = await CollectionService.CollectionService.DeleteCollection(id);
    await this.loadAll();
    return (deletedRequestIds ?? []).map(id => Number(id));
  }
}
