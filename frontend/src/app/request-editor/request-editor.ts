import { Component, computed, effect, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { WorkspaceStateService } from '../core/services/workspace-state.service';
import { RequestApiService, type HttpRequest, type HttpRequestSummary } from '../core/services/request.service';
import { ApiRequestsService } from '../core/services/api-requests.service';
import { FavouriteApiService } from '../core/services/favourite.service';
import { TagApiService, type Tag } from '../core/services/tag.service';
import { EnvironmentVariableApiService } from '../core/services/environment-variable.service';
import { BodyEditor } from '../body-editor/body-editor';

@Component({
  selector: 'app-request-editor',
  imports: [FormsModule, BodyEditor],
  templateUrl: './request-editor.html',
  styleUrl: './request-editor.scss',
})
export class RequestEditor {
  protected readonly state = inject(WorkspaceStateService);
  private readonly requestApi = inject(RequestApiService);
  private readonly apiRequests = inject(ApiRequestsService);
  private readonly favouriteApi = inject(FavouriteApiService);
  private readonly tagApi = inject(TagApiService);
  private readonly envVarApi = inject(EnvironmentVariableApiService);

  protected readonly requestTags = this.tagApi.requestTags;
  protected readonly environmentVariables = computed(() =>
    this.envVarApi.variables().map((v) => ({ key: v.key, value: v.value })),
  );

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
      const req = this.state.selectedRequest();
      this.draftRequest.set(req ? { ...req } : null);
    });

    effect(() => {
      const env = this.state.selectedEnvironment();
      if (env) {
        this.envVarApi.loadForEnvironment(env.id);
      } else {
        this.envVarApi.variables.set([]);
      }
    });
  }

  async updateRequestField<K extends keyof HttpRequest>(field: K, value: HttpRequest[K]): Promise<void> {
    const draft = this.draftRequest();
    const collection = this.state.selectedCollection();
    if (!draft || !collection) return;

    const updated = { ...draft, [field]: value };
    this.draftRequest.set(updated);

    try {
      const saved = await this.apiRequests.update({ ...updated, collection_id: collection.id });
      this.state.selectedRequest.set(saved);

      const favList = this.favouriteApi.requests();
      const favIndex = favList.findIndex(r => r.id === saved.id);
      if (favIndex !== -1) {
        const newFavList = [...favList];
        newFavList[favIndex] = saved;
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

  async addTagToRequest(req: HttpRequest | HttpRequestSummary, tagName: string): Promise<void> {
    const name = tagName.trim();
    const project = this.state.selectedProject();
    if (!name || !project) return;

    try {
      await this.tagApi.addTagToRequest(req.id, project.id, name);
      this.closeTagInput();
    } catch (err) {
      console.error(err);
    }
  }

  async removeTagFromRequest(req: HttpRequest | HttpRequestSummary, tagName: string, event: MouseEvent): Promise<void> {
    event.stopPropagation();
    try {
      await this.tagApi.removeTagFromRequest(req.id, tagName);
    } catch (err) {
      console.error(err);
    }
  }
}
