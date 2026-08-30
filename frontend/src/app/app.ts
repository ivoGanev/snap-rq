import { AfterViewInit, Component, effect, inject, OnInit, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { WML } from '@wailsio/runtime';
import { WailsService } from './wails.service';
import { RequestApiService, type HttpRequest, type HttpResponse } from './core/services/request.service';
import { CollectionApiService, type Collection } from './core/services/collection.service';
import { ProjectApiService, type Project } from './core/services/project.service';
import { EnvironmentApiService, type Environment } from './core/services/environment.service';
import { EnvironmentVariableApiService, type EnvironmentVariable } from './core/services/environment-variable.service';
import * as EnvironmentService from '../../bindings/snap-rq/backend/services';

type RightPanelMode = 'response' | 'edit';

@Component({
  selector: 'app-root',
  imports: [FormsModule],
  templateUrl: './app.html',
  styleUrl: './app.scss',
})
export class App implements OnInit, AfterViewInit {
  private readonly wails = inject(WailsService);
  private readonly requestApi = inject(RequestApiService);
  private readonly collectionApi = inject(CollectionApiService);
  private readonly projectApi = inject(ProjectApiService);
  private readonly environmentApi = inject(EnvironmentApiService);
  private readonly variableApi = inject(EnvironmentVariableApiService);

  readonly currentTime = this.wails.currentTime;
  readonly collections = this.collectionApi.collections;
  readonly requests = this.requestApi.requests;
  readonly responses = this.requestApi.responses;
  readonly projects = this.projectApi.projects;
  readonly environments = this.environmentApi.environments;
  readonly variables = this.variableApi.variables;

  readonly loading = signal(false);
  readonly error = signal<string | null>(null);
  readonly selectedProject = signal<Project | null>(null);
  readonly selectedEnvironment = signal<Environment | null>(null);
  readonly selectedCollection = signal<Collection | null>(null);
  readonly selectedRequest = signal<HttpRequest | null>(null);
  readonly selectedResponse = signal<HttpResponse | null>(null);
  readonly rightPanelMode = signal<RightPanelMode>('response');
  readonly draftRequest = signal<HttpRequest | null>(null);
  readonly variablesOverlayOpen = signal(false);

  constructor() {
    effect(() => {
      const req = this.selectedRequest();
      if (req) {
        this.loadResponses(req.id);
        this.draftRequest.set({ ...req });
      } else {
        this.requestApi.responses.set([]);
        this.selectedResponse.set(null);
        this.draftRequest.set(null);
        this.rightPanelMode.set('response');
      }
    });
  }

  async ngOnInit(): Promise<void> {
    await this.loadProjects();
  }

  ngAfterViewInit(): void {
    WML.Enable();
  }

  async loadProjects(): Promise<void> {
    this.loading.set(true);
    this.error.set(null);
    try {
      await this.projectApi.loadAll();
      const all = this.projects();
      if (all.length > 0) {
        this.selectedProject.set(all[0]);
        await this.loadEnvironments(all[0].id);
      }
      await this.collectionApi.loadAll();
      const collections = this.collections();
      if (collections.length > 0) {
        this.selectCollection(collections[0]);
      }
    } catch (err) {
      console.error(err);
      this.error.set('Failed to load projects.');
    } finally {
      this.loading.set(false);
    }
  }

  async loadEnvironments(projectId: number): Promise<void> {
    try {
      await this.environmentApi.loadForProject(projectId);
      const all = this.environments();
      this.selectedEnvironment.set(all.length > 0 ? all[0] : null);
    } catch (err) {
      console.error(err);
    }
  }

  async selectEnvironment(environment: Environment): Promise<void> {
    this.selectedEnvironment.set(environment);
  }

  onEnvironmentChange(environmentId: number): void {
    const env = this.environments().find(e => e.id === environmentId);
    if (env) {
      this.selectEnvironment(env);
    }
  }

  async selectCollection(collection: Collection): Promise<void> {
    this.selectedCollection.set(collection);
    this.selectedRequest.set(null);
    this.selectedResponse.set(null);
    this.rightPanelMode.set('response');
    try {
      await this.requestApi.loadForCollection(collection.id);
    } catch (err) {
      console.error(err);
    }
  }

  async loadResponses(requestId: number): Promise<void> {
    try {
      await this.requestApi.loadResponsesForRequest(requestId);
      const all = this.responses();
      this.selectedResponse.set(all.length > 0 ? all[0] : null);
    } catch (err) {
      console.error(err);
    }
  }

  selectRequest(req: HttpRequest): void {
    this.selectedRequest.set(req);
    this.rightPanelMode.set('response');
  }

  selectResponse(resp: HttpResponse): void {
    this.selectedResponse.set(resp);
  }

  setRightPanelMode(mode: RightPanelMode): void {
    this.rightPanelMode.set(mode);
    if (mode === 'edit') {
      const req = this.selectedRequest();
      this.draftRequest.set(req ? { ...req } : null);
    }
  }

  async updateRequestField<K extends keyof HttpRequest>(field: K, value: HttpRequest[K]): Promise<void> {
    const draft = this.draftRequest();
    const collection = this.selectedCollection();
    if (!draft || !collection) return;

    const updated = { ...draft, [field]: value };
    this.draftRequest.set(updated);

    try {
      await this.requestApi.update({ ...updated, collection_id: collection.id });
      this.selectedRequest.set(updated);

      const list = this.requests();
      const index = list.findIndex(r => r.id === updated.id);
      if (index !== -1) {
        const newList = [...list];
        newList[index] = updated;
        this.requestApi.requests.set(newList);
      }
    } catch (err) {
      console.error(err);
      this.error.set('Failed to save request change.');
    }
  }

  async sendRequest(req: HttpRequest, event: MouseEvent): Promise<void> {
    event.stopPropagation();
    this.loading.set(true);
    try {
      const environmentId = this.selectedEnvironment()?.id ?? 0;
      const resp = await this.requestApi.execute(req.id, environmentId);
      if (this.selectedCollection()) {
        await this.requestApi.loadForCollection(this.selectedCollection()!.id);
      }
      if (this.selectedRequest()?.id === req.id) {
        await this.loadResponses(req.id);
        this.selectedResponse.set(resp);
        this.rightPanelMode.set('response');
      }
    } catch (err) {
      console.error(err);
      this.error.set('Failed to send request.');
    } finally {
      this.loading.set(false);
    }
  }

  async deleteEnvironment(): Promise<void> {
    const env = this.selectedEnvironment();
    const project = this.selectedProject();
    if (!env || !project) return;

    this.loading.set(true);
    try {
      await EnvironmentService.EnvironmentService.DeleteEnvironment(env.id);
      await this.loadEnvironments(project.id);
      this.closeVariablesOverlay();
    } catch (err) {
      console.error(err);
      this.error.set('Failed to delete environment.');
    } finally {
      this.loading.set(false);
    }
  }

  openVariablesOverlay(): void {
    this.variablesOverlayOpen.set(true);
    const env = this.selectedEnvironment();
    if (env) {
      this.loadVariables(env.id);
    }
  }

  closeVariablesOverlay(): void {
    this.variablesOverlayOpen.set(false);
  }

  async loadVariables(environmentId: number): Promise<void> {
    try {
      await this.variableApi.loadForEnvironment(environmentId);
    } catch (err) {
      console.error(err);
    }
  }

  async updateVariableField<K extends keyof EnvironmentVariable>(
    variable: EnvironmentVariable,
    field: K,
    value: EnvironmentVariable[K],
  ): Promise<void> {
    const updated = { ...variable, [field]: value };

    const list = this.variables();
    const index = list.findIndex(v => v.id === updated.id);
    if (index !== -1) {
      const newList = [...list];
      newList[index] = updated;
      this.variableApi.variables.set(newList);
    }

    try {
      await this.variableApi.update(updated);
    } catch (err) {
      console.error(err);
      this.error.set('Failed to save variable.');
    }
  }

  async addVariable(): Promise<void> {
    const env = this.selectedEnvironment();
    if (!env) return;

    try {
      const created = await this.variableApi.create({
        environment_id: env.id,
        key: 'NEW_KEY',
        value: '',
      });
      this.variableApi.variables.update(list => [...list, created]);
    } catch (err) {
      console.error(err);
      this.error.set('Failed to add variable.');
    }
  }

  async deleteVariable(variable: EnvironmentVariable, event: MouseEvent): Promise<void> {
    event.stopPropagation();
    try {
      await this.variableApi.delete(variable.id);
      this.variableApi.variables.update(list => list.filter(v => v.id !== variable.id));
    } catch (err) {
      console.error(err);
      this.error.set('Failed to delete variable.');
    }
  }
}
