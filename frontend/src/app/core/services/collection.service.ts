import { Injectable, inject, signal } from '@angular/core';
import { CommandApiService } from './command.service';

export interface CollectionAppearance {
  id: number;
  collection_id: number;
  appearance_type: string;
  appearance_value: string;
}

export interface Collection {
  id: number;
  project_id: number;
  name: string;
  appearance: CollectionAppearance;
}

/**
 * The app's typed API for collections: every operation runs as a command.
 */
@Injectable({ providedIn: 'root' })
export class CollectionApiService {
  private readonly commands = inject(CommandApiService);

  readonly collections = signal<Collection[]>([]);

  async loadForProject(projectId: number): Promise<void> {
    const all = await this.commands.execute<Collection[]>('collection.list', { project_id: projectId });
    this.collections.set(all ?? []);
  }

  async create(collection: Omit<Collection, 'id' | 'appearance'>): Promise<Collection> {
    const created = await this.commands.execute<Collection>('collection.create', collection);
    await this.loadForProject(created.project_id);
    return created;
  }

  async update(collection: Collection): Promise<Collection> {
    return this.commands.execute<Collection>('collection.update', collection);
  }

  async updateAppearance(collectionId: number, appearance: Omit<CollectionAppearance, 'id' | 'collection_id'>): Promise<CollectionAppearance> {
    return this.commands.execute<CollectionAppearance>('collection.set_appearance', {
      collection_id: collectionId, ...appearance,
    });
  }

  async delete(id: number): Promise<void> {
    await this.commands.execute('collection.delete', { id });
  }
}