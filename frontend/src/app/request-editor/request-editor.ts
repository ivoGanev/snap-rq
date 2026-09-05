import { Component, computed, effect, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { WorkspaceStateService } from '../core/services/workspace-state.service';
import { RequestApiService, type HttpRequest } from '../core/services/request.service';
import { FavouriteApiService } from '../core/services/favourite.service';
import { TagApiService, type Tag } from '../core/services/tag.service';

@Component({
  selector: 'app-request-editor',
  imports: [FormsModule],
  templateUrl: './request-editor.html',
  styleUrl: './request-editor.scss',
})
export class RequestEditor {
  protected readonly state = inject(WorkspaceStateService);
  private readonly requestApi = inject(RequestApiService);
  private readonly favouriteApi = inject(FavouriteApiService);
  private readonly tagApi = inject(TagApiService);

  protected readonly requestTags = this.tagApi.requestTags;

  readonly draftRequest = signal<HttpRequest | null>(null);
  readonly tagInputOpen = signal(false);
  readonly newTagName = signal('');

  readonly tagSuggestions = computed<Tag[]>(() => {
    const query = this.newTagName().trim().toLowerCase();
    const tags = this.tagApi.allTags();
    const selected = this.state.selectedRequest();
    const existing = selected ? new Set(this.requestTags()[selected.id] ?? []) : new Set<string>();
    if (!query) {
      return tags.filter(tag => !existing.has(tag.name)).slice(0, 6);
    }
    return tags
      .filter(tag => tag.name.toLowerCase().includes(query) && !existing.has(tag.name))
      .slice(0, 6);
  });

  constructor() {
    effect(() => {
      this.state.rightPanelMode();
      const req = this.state.selectedRequest();
      this.draftRequest.set(req ? { ...req } : null);
    });
  }

  async updateRequestField<K extends keyof HttpRequest>(field: K, value: HttpRequest[K]): Promise<void> {
    const draft = this.draftRequest();
    const collection = this.state.selectedCollection();
    if (!draft || !collection) return;

    const updated = { ...draft, [field]: value };
    this.draftRequest.set(updated);

    try {
      await this.requestApi.update({ ...updated, collection_id: collection.id });
      this.state.selectedRequest.set(updated);

      const list = this.requestApi.requests();
      const index = list.findIndex(r => r.id === updated.id);
      if (index !== -1) {
        const newList = [...list];
        newList[index] = updated;
        this.requestApi.requests.set(newList);
      }

      const favList = this.favouriteApi.requests();
      const favIndex = favList.findIndex(r => r.id === updated.id);
      if (favIndex !== -1) {
        const newFavList = [...favList];
        newFavList[favIndex] = updated;
        this.favouriteApi.requests.set(newFavList);
      }
    } catch (err) {
      console.error(err);
    }
  }

  openTagInput(): void {
    this.tagInputOpen.set(true);
    this.newTagName.set('');
  }

  closeTagInput(): void {
    this.tagInputOpen.set(false);
    this.newTagName.set('');
  }

  async addTagToRequest(req: HttpRequest, tagName: string): Promise<void> {
    const name = tagName.trim();
    if (!name) return;

    try {
      await this.tagApi.addTagToRequest(req.id, name);
      this.closeTagInput();
    } catch (err) {
      console.error(err);
    }
  }

  async removeTagFromRequest(req: HttpRequest, tagName: string, event: MouseEvent): Promise<void> {
    event.stopPropagation();
    try {
      await this.tagApi.removeTagFromRequest(req.id, tagName);
    } catch (err) {
      console.error(err);
    }
  }
}
