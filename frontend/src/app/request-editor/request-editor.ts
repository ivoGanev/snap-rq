import { Component, computed, effect, inject, input, signal } from '@angular/core';
import { RequestApiService, type HttpRequest, type HttpRequestSummary } from '../core/services/request.service';
import { FormsModule } from '@angular/forms';
import { Select, type SelectOption } from '../select/select';
import { WorkspaceStateService } from '../core/services/workspace-state.service';

import { FavouriteApiService } from '../core/services/favourite.service';
import { ScriptApiService, type Script } from '../core/services/script.service';
import { RequestBodyEditor, type BodyKind } from '../request-body-editor/request-body-editor';
import { RequestHeadersEditor } from '../request-headers-editor/request-headers-editor';
import { CodeEditor } from '../code-editor/code-editor';

@Component({
  selector: 'app-request-editor',
  imports: [FormsModule, Select, RequestBodyEditor, RequestHeadersEditor, CodeEditor],
  templateUrl: './request-editor.html',
  styleUrl: './request-editor.scss',
})
export class RequestEditor {
  protected readonly methods: SelectOption[] = ['GET', 'POST', 'PUT', 'DELETE', 'PATCH', 'HEAD', 'OPTIONS']
    .map((method) => ({ value: method, label: method }));
  protected readonly state = inject(WorkspaceStateService);
  private readonly requestApi = inject(RequestApiService);
  private readonly favouriteApi = inject(FavouriteApiService);
  private readonly scriptApi = inject(ScriptApiService);

  protected readonly requestScripts = this.scriptApi.requestScripts;
  protected readonly environmentVariables = this.state.environmentVariables;

  readonly showScriptInput = input<boolean>(true);
  readonly draftRequest = signal<HttpRequest | null>(null);
  readonly scriptInputOpen = signal(false);
  readonly newScriptName = signal('');

  readonly bodyEditorType = computed<BodyKind>(() => {
    const type = this.draftRequest()?.body_type;
    if (type === 'form-data' || type === 'text') return type;
    return 'json';
  });

  readonly scriptSuggestions = computed<Script[]>(() => {
    const query = this.newScriptName().trim().toLowerCase();
    const scripts = this.scriptApi.allScripts();
    const selected = this.state.selectedRequest();
    const existing = selected ? new Set(this.requestScripts()[selected.id] ?? []) : new Set<string>();
    if (!query) {
      return scripts.filter((script) => !existing.has(script.name)).slice(0, 6);
    }
    return scripts
      .filter((script) => script.name.toLowerCase().includes(query) && !existing.has(script.name))
      .slice(0, 6);
  });

  constructor() {
    effect(() => {
      const req = this.state.selectedRequest();
      this.draftRequest.set(req ? { ...req } : null);
    });
  }

  async updateRequestField<K extends keyof HttpRequest>(
    field: K,
    value: HttpRequest[K],
  ): Promise<void> {
    const draft = this.draftRequest();
    if (!draft) return;

    const updated = { ...draft, [field]: value };
    this.draftRequest.set(updated);

    try {
      const saved = await this.requestApi.update({
        ...updated,
        collection_id: draft.collection_id,
      });
      this.state.selectedRequest.set(saved);

      const favList = this.favouriteApi.requests();
      const favIndex = favList.findIndex((r) => r.id === saved.id);
      if (favIndex !== -1) {
        // `saved` carries the collection-scoped membership fields; preserve the
        // favourite-scoped ones so editing a request doesn't reset its colour
        // or position within the favourite collection.
        const existing = favList[favIndex];
        const newFavList = [...favList];
        newFavList[favIndex] = {
          ...saved,
          user_sort_order: existing.user_sort_order,
          user_color: existing.user_color,
        };
        this.favouriteApi.requests.set(newFavList);
      }
    } catch (err) {
      console.error(err);
    }
  }

  async onBodyTypeChange(type: BodyKind): Promise<void> {
    const draft = this.draftRequest();
    if (!draft) return;

    await this.updateRequestField('body_type', type);
    // Keep body_form so switching back to form-data restores the saved rows.
    if (type === 'form-data' && !draft.body_form) {
      await this.updateRequestField('body_form', '[]');
    }
  }

  openScriptInput(): void {
    this.scriptInputOpen.set(true);
    this.newScriptName.set('');
  }

  closeScriptInput(): void {
    this.scriptInputOpen.set(false);
    this.newScriptName.set('');
  }

  async addScriptToRequest(req: HttpRequest | HttpRequestSummary, scriptName: string): Promise<void> {
    const name = scriptName.trim();
    const project = this.state.selectedProject();
    if (!name || !project) return;

    try {
      await this.scriptApi.attachScript(req.id, project.id, name);
      this.closeScriptInput();
    } catch (err) {
      console.error(err);
    }
  }

  async removeScriptFromRequest(
    req: HttpRequest | HttpRequestSummary,
    scriptName: string,
    event: MouseEvent,
  ): Promise<void> {
    event.stopPropagation();
    try {
      await this.scriptApi.detachScript(req.id, scriptName);
    } catch (err) {
      console.error(err);
    }
  }
}
