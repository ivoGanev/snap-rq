import { Injectable, inject, signal } from '@angular/core';
import { CommandApiService } from './command.service';
import { ChangeBus } from './change-bus.service';
import type { LayoutItem } from '../workspace/collection-layout';

/** A named group of requests inside one collection. */
export interface RequestGroupRecord {
  id: number;
  collection_id: number;
  name: string;
  /** A colour slot (see request-colors), or null. */
  color: string | null;
  sort_order: number;
}

/** The project's request groups; every change runs as an undoable command. */
@Injectable({ providedIn: 'root' })
export class GroupApiService {
  private readonly commands = inject(CommandApiService);
  private readonly bus = inject(ChangeBus);
  private projectId: number | null = null;

  readonly groups = signal<RequestGroupRecord[]>([]);

  constructor() {
    this.bus.changesFor('group').subscribe(() => {
      if (this.projectId) void this.loadForProject(this.projectId);
    });
  }

  async loadForProject(projectId: number): Promise<void> {
    this.projectId = projectId;
    const all = await this.commands.execute<RequestGroupRecord[]>('group.list', { project_id: projectId });
    this.groups.set(all ?? []);
  }

  forCollection(collectionId: number): RequestGroupRecord[] {
    return this.groups().filter((g) => g.collection_id === collectionId);
  }

  create(collectionId: number, name: string, requestIds: number[]): Promise<RequestGroupRecord> {
    return this.commands.execute<RequestGroupRecord>('group.create', {
      collection_id: collectionId,
      name,
      request_ids: requestIds,
    });
  }

  async update(group: RequestGroupRecord): Promise<void> {
    this.groups.update((all) => all.map((g) => (g.id === group.id ? group : g)));
    await this.commands.execute('group.update', group);
  }

  /** Moves the requests into the group, or out of any group when `groupId` is null. */
  async setRequests(groupId: number | null, requestIds: number[]): Promise<void> {
    await this.commands.execute('group.set_requests', { group_id: groupId ?? 0, request_ids: requestIds });
  }

  /** Saves a collection's layout after a drag: every item's place and every request's group. */
  async arrange(collectionId: number, items: LayoutItem[]): Promise<void> {
    const position = new Map<number, number>();
    items.forEach((it, index) => {
      if (it.kind === 'group') position.set(it.id, index);
    });
    this.groups.update((all) =>
      all.map((g) => (position.has(g.id) ? { ...g, sort_order: position.get(g.id)! } : g)),
    );
    await this.commands.execute('group.arrange', {
      collection_id: collectionId,
      items: items.map((it) =>
        it.kind === 'group' ? { kind: 'group', id: it.id } : { kind: 'request', id: it.id, group_id: it.groupId ?? 0 },
      ),
    });
  }

  async delete(groupId: number): Promise<void> {
    await this.commands.execute('group.delete', { id: groupId });
  }
}
