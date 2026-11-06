import { Injectable, inject, signal } from '@angular/core';
import { CommandApiService } from './command.service';
import { ChangeBus } from './change-bus.service';
import type { HttpRequestSummary } from '../models/http';

export interface TagAppearance {
  id: number;
  tag_id: number;
  appearance_type: string;
  appearance_value: string;
}

export interface Tag {
  id: number;
  project_id: number;
  name: string;
  appearance: TagAppearance;
}

/**
 * The app's typed API for tags: every operation runs as a command.
 */
@Injectable({ providedIn: 'root' })
export class TagApiService {
  private readonly commands = inject(CommandApiService);
  private readonly bus = inject(ChangeBus);
  private projectId: number | null = null;

  readonly allTags = signal<Tag[]>([]);
  readonly requestTags = signal<Record<number, string[] | undefined>>({});

  constructor() {
    this.bus.changesFor('tag').subscribe(() => {
      if (this.projectId) void this.loadTagsForProject(this.projectId);
    });
  }

  async loadTagsForProject(projectId: number): Promise<void> {
    this.projectId = projectId;
    const tags = await this.commands.execute<Tag[]>('tag.list', { project_id: projectId });
    this.allTags.set(tags ?? []);
  }

  async loadTagsForRequests(requests: HttpRequestSummary[]): Promise<void> {
    if (requests.length === 0) {
      this.requestTags.set({});
      return;
    }
    const raw = await this.commands.execute<Record<string, string[]>>('tag.list_for_requests', {
      request_ids: requests.map(r => r.id),
    });
    const mapped: Record<number, string[]> = {};
    for (const [key, value] of Object.entries(raw ?? {})) {
      mapped[Number(key)] = value ?? [];
    }
    this.requestTags.set(mapped);
  }

  async addTagToRequest(requestId: number, projectId: number, tagName: string): Promise<Tag> {
    const tag = await this.commands.execute<Tag>('tag.add', {
      request_id: requestId, project_id: projectId, tag_name: tagName,
    });
    this.allTags.update(list => {
      if (list.some(t => t.id === tag.id)) return list;
      return [...list, tag];
    });
    if (requestId !== 0) {
      this.requestTags.update(map => ({
        ...map,
        [requestId]: [...(map[requestId] ?? []), tag.name],
      }));
    }
    return tag;
  }

  async removeTagFromRequest(requestId: number, tagName: string): Promise<void> {
    await this.commands.execute('tag.remove', { request_id: requestId, tag_name: tagName });
    this.requestTags.update(map => ({
      ...map,
      [requestId]: (map[requestId] ?? []).filter(t => t !== tagName),
    }));
  }

  async renameTag(projectId: number, oldName: string, newName: string): Promise<Tag> {
    const tag = await this.commands.execute<Tag>('tag.rename', {
      project_id: projectId, old_name: oldName, new_name: newName,
    });
    await this.loadTagsForProject(projectId);
    return tag;
  }

  async deleteTag(projectId: number, tagName: string): Promise<void> {
    await this.commands.execute('tag.delete', { project_id: projectId, tag_name: tagName });
    this.allTags.update(list => list.filter(t => t.name !== tagName));
    this.requestTags.update(map => {
      const next: Record<number, string[] | undefined> = {};
      for (const [requestId, tags] of Object.entries(map)) {
        next[Number(requestId)] = tags?.filter(t => t !== tagName);
      }
      return next;
    });
  }

  async getRequestsForTag(projectId: number, tagName: string): Promise<HttpRequestSummary[]> {
    const result = await this.commands.execute<HttpRequestSummary[]>('tag.list_requests', {
      project_id: projectId, tag_name: tagName,
    });
    return result ?? [];
  }

  async updateRequestOrder(projectId: number, tagName: string, orderedRequestIds: number[]): Promise<void> {
    await this.commands.execute('tag.reorder', {
      project_id: projectId, tag_name: tagName, ordered_request_ids: orderedRequestIds,
    });
  }

  async updateRequestColor(projectId: number, tagName: string, requestIds: number[], color: string | null): Promise<void> {
    await this.commands.execute('tag.set_color', {
      project_id: projectId, tag_name: tagName, request_ids: requestIds, color,
    });
  }

  /** Updates a tag's appearance (icon or colour). */
  async updateAppearance(tagId: number, appearance: Omit<TagAppearance, 'id' | 'tag_id'>): Promise<TagAppearance> {
    const updated = await this.commands.execute<TagAppearance>('tag.set_appearance', {
      tag_id: tagId, ...appearance,
    });
    this.allTags.update(list => list.map(t => (t.id === tagId ? { ...t, appearance: updated } : t)));
    return updated;
  }
}