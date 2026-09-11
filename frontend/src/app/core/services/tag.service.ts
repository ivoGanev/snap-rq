import { Injectable, signal } from '@angular/core';
import * as TagService from '../../../../bindings/snap-rq/backend/services';
import type { Tag, TagAppearance, HttpRequest } from '../../../../bindings/snap-rq/backend/models';

export type { Tag, TagAppearance };

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

  async updateAppearance(
    tagId: number,
    appearance: Omit<TagAppearance, 'id' | 'tag_id'>,
  ): Promise<TagAppearance> {
    const updated = await TagService.TagService.UpdateTagAppearance(
      tagId,
      { ...appearance, id: 0, tag_id: tagId } as TagAppearance,
    );
    this.allTags.update(list =>
      list.map(t => (t.id === tagId ? { ...t, appearance: updated } : t)),
    );
    return updated;
  }

  async renameTag(oldName: string, newName: string): Promise<Tag> {
    const normalised = newName.trim().toLowerCase();
    if (!normalised || normalised === oldName.toLowerCase()) {
      throw new Error('Invalid tag name');
    }

    const oldTag = this.allTags().find((t) => t.name === oldName);
    const tag = await TagService.TagService.AddTagToRequest(0, normalised);
    // Preserve the original tag's appearance on the renamed tag.
    if (oldTag && tag.id !== oldTag.id) {
      await this.updateAppearance(tag.id, {
        appearance_type: oldTag.appearance.appearance_type,
        appearance_value: oldTag.appearance.appearance_value,
      });
    }
    // Move all request associations from old tag to new tag.
    const requests = await this.getRequestsForTag(oldName);
    for (const req of requests) {
      await TagService.TagService.AddTagToRequest(req.id, normalised);
      await TagService.TagService.RemoveTagFromRequest(req.id, oldName);
    }
    await TagService.TagService.DeleteTag(oldName);
    await this.loadAllTags();
    return tag;
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
