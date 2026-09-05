import { Injectable, signal } from '@angular/core';
import * as TagService from '../../../../bindings/snap-rq/backend/services';
import type { Tag, HttpRequest } from '../../../../bindings/snap-rq/backend/models';

export type { Tag };

@Injectable({ providedIn: 'root' })
export class TagApiService {
  readonly allTags = signal<Tag[]>([]);
  readonly requestTags = signal<Record<number, string[] | undefined>>({});

  async loadAllTags(): Promise<void> {
    const tags = await TagService.TagService.GetAllTags();
    this.allTags.set(tags ?? []);
  }

  async loadTagsForRequests(requests: HttpRequest[]): Promise<void> {
    if (requests.length === 0) {
      this.requestTags.set({});
      return;
    }

    const ids = requests.map(r => r.id);
    const raw = await TagService.TagService.GetTagsForRequests(ids);
    const mapped: Record<number, string[]> = {};
    for (const [key, value] of Object.entries(raw ?? {})) {
      mapped[Number(key)] = value ?? [];
    }
    this.requestTags.set(mapped);
  }

  async addTagToRequest(requestId: number, tagName: string): Promise<Tag> {
    const tag = await TagService.TagService.AddTagToRequest(requestId, tagName);
    this.allTags.update(list => {
      if (list.some(t => t.id === tag.id)) {
        return list;
      }
      return [...list, tag];
    });
    this.requestTags.update(map => ({
      ...map,
      [requestId]: [...(map[requestId] ?? []), tag.name],
    }));
    return tag;
  }

  async removeTagFromRequest(requestId: number, tagName: string): Promise<void> {
    await TagService.TagService.RemoveTagFromRequest(requestId, tagName);
    this.requestTags.update(map => ({
      ...map,
      [requestId]: (map[requestId] ?? []).filter(t => t !== tagName),
    }));
  }

  async deleteTag(tagName: string): Promise<void> {
    await TagService.TagService.DeleteTag(tagName);
    this.allTags.update(list => list.filter(t => t.name !== tagName));
    this.requestTags.update(map => {
      const next: Record<number, string[] | undefined> = {};
      for (const [requestId, tags] of Object.entries(map)) {
        next[Number(requestId)] = tags?.filter(t => t !== tagName);
      }
      return next;
    });
  }

  async getRequestsForTag(tagName: string): Promise<HttpRequest[]> {
    return (await TagService.TagService.GetRequestsForTag(tagName)) ?? [];
  }
}
