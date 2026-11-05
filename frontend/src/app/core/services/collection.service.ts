import { Injectable, inject, signal } from '@angular/core';
import { CommandApiService } from './command.service';
import { ChangeBus } from './change-bus.service';

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
  private readonly bus = inject(ChangeBus);
  private projectId: number | null = null;

  readonly collections = signal<Collection[]>([]);

  constructor() {
    this.bus.changesFor('collection').subscribe(() => {
      if (this.projectId) void this.loadForProject(this.projectId);
    });
  }

  async loadForProject(projectId: number): Promise<void> {
    this.projectId = projectId;
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

  /** Deletes a collection and returns the ids of the requests that died with it. */
  async delete(id: number): Promise<number[]> {
    const result = await this.commands.execute<{ request_ids: number[] }>('collection.delete', { id });
    this.collections.update(list => list.filter(collection => collection.id !== id));
    return result?.request_ids ?? [];
  }
}