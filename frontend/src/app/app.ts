import { AfterViewInit, Component, computed, effect, inject, OnInit, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { WML } from '@wailsio/runtime';
import { WailsService } from './wails.service';
import { RequestApiService, type HttpRequest, type HttpResponse } from './core/services/request.service';
import { CollectionApiService, type Collection } from './core/services/collection.service';
import { ProjectApiService, type Project } from './core/services/project.service';
import { EnvironmentApiService, type Environment } from './core/services/environment.service';
import { EnvironmentVariableApiService, type EnvironmentVariable } from './core/services/environment-variable.service';
import { FavouriteApiService, type FavouriteCollection } from './core/services/favourite.service';
import { SelectionStateService } from './core/services/selection-state.service';
import { CollectionAppearanceService } from './core/services/collection-appearance.service';
import { IconManifestService } from './core/services/icon-manifest.service';
import * as EnvironmentService from '../../bindings/snap-rq/backend/services';

type RightPanelMode = 'response' | 'edit';
type SidebarSection = 'collections' | 'favourites';

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
  private readonly favouriteApi = inject(FavouriteApiService);
  private readonly selectionState = inject(SelectionStateService);
  readonly collectionAppearance = inject(CollectionAppearanceService);
  readonly iconManifest = inject(IconManifestService);

  readonly currentTime = this.wails.currentTime;
  readonly collections = this.collectionApi.collections;
  readonly requests = this.requestApi.requests;
  readonly responses = this.requestApi.responses;
  readonly projects = this.projectApi.projects;
  readonly environments = this.environmentApi.environments;
  readonly variables = this.variableApi.variables;
  readonly favouriteCollections = this.favouriteApi.collections;
  readonly favouriteRequests = this.favouriteApi.requests;
  readonly favouriteMembership = this.favouriteApi.membership;

  readonly loading = signal(false);
  readonly error = signal<string | null>(null);
  readonly selectedProject = signal<Project | null>(null);
  readonly selectedEnvironment = signal<Environment | null>(null);
  readonly selectedCollection = signal<Collection | null>(null);
  readonly selectedFavouriteCollection = signal<FavouriteCollection | null>(null);
  readonly selectedRequest = signal<HttpRequest | null>(null);
  readonly selectedResponse = signal<HttpResponse | null>(null);
  readonly rightPanelMode = signal<RightPanelMode>('response');
  readonly draftRequest = signal<HttpRequest | null>(null);
  readonly variablesOverlayOpen = signal(false);
  readonly collectionsExpanded = signal(true);
  readonly favouritesExpanded = signal(false);
  readonly activeSection = signal<SidebarSection>('collections');
  readonly favouritePopupOpen = signal(false);
  readonly favouritePopupRequest = signal<HttpRequest | null>(null);
  readonly newFavouriteName = signal('');
  readonly newRequestPopupOpen = signal(false);
  readonly newRequestName = signal('My new snappy API');
  readonly newRequestUrl = signal('');
  readonly newRequestMethod = signal('GET');
  readonly requestContextMenuOpen = signal(false);
  readonly requestContextMenuX = signal(0);
  readonly requestContextMenuY = signal(0);
  readonly requestContextMenuTarget = signal<HttpRequest | null>(null);
  readonly collectionContextMenuOpen = signal(false);
  readonly collectionContextMenuX = signal(0);
  readonly collectionContextMenuY = signal(0);
  readonly collectionContextMenuTarget = signal<Collection | null>(null);
  readonly collectionAppearancePopupOpen = signal(false);
  readonly collectionAppearanceTarget = signal<Collection | null>(null);
  readonly collectionAppearanceTab = signal<'icon' | 'color'>('icon');
  readonly newCollectionPopupOpen = signal(false);
  readonly newCollectionName = signal('');
  readonly requestSearchQuery = signal('');
  readonly projectEditorOpen = signal(false);
  readonly newProjectName = signal('');
  readonly projectDeleteConfirmOpen = signal(false);
  readonly projectPendingDelete = signal<Project | null>(null);

  readonly filteredActiveRequests = computed<HttpRequest[]>(() => {
    const query = this.requestSearchQuery().trim().toLowerCase();
    const requests = this.activeRequests();
    if (!query) return requests;
    return requests.filter(
      req =>
        req.name.toLowerCase().includes(query) ||
        req.url.toLowerCase().includes(query) ||
        req.method.toLowerCase().includes(query),
    );
  });

  readonly activeRequests = computed<HttpRequest[]>(() =>
    this.activeSection() === 'favourites' ? this.favouriteRequests() : this.requests(),
  );

  readonly activeCollectionName = computed<string | null>(() =>
    this.activeSection() === 'favourites'
      ? this.selectedFavouriteCollection()?.name ?? null
      : this.selectedCollection()?.name ?? null,
  );

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
        await this.selectProject(all[0]);
      } else {
        this.selectedProject.set(null);
        this.selectedEnvironment.set(null);
        this.selectedCollection.set(null);
        this.selectedFavouriteCollection.set(null);
        this.selectedRequest.set(null);
        this.selectedResponse.set(null);
        this.collectionApi.collections.set([]);
        this.requestApi.requests.set([]);
        this.favouriteApi.collections.set([]);
        this.favouriteApi.requests.set([]);
      }
    } catch (err) {
      console.error(err);
      this.error.set('Failed to load projects.');
    } finally {
      this.loading.set(false);
    }
  }

  async selectProject(project: Project): Promise<void> {
    this.selectedProject.set(project);
    this.selectedCollection.set(null);
    this.selectedFavouriteCollection.set(null);
    this.selectedRequest.set(null);
    this.selectedResponse.set(null);
    this.requestApi.requests.set([]);
    this.rightPanelMode.set('response');
    this.requestSearchQuery.set('');

    await this.loadEnvironments(project.id);
    await this.favouriteApi.loadCollectionsForProfile(project.profile_id);
    await this.collectionApi.loadForProject(project.id);

    const collections = this.collections();
    if (collections.length > 0) {
      await this.selectCollection(collections[0]);
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

  selectEnvironment(environment: Environment): void {
    this.selectedEnvironment.set(environment);
  }

  onEnvironmentChange(environmentId: number): void {
    const env = this.environments().find(e => e.id === environmentId);
    if (env) {
      this.selectEnvironment(env);
    }
  }

  toggleCollectionsExpanded(): void {
    this.collectionsExpanded.update(v => !v);
  }

  toggleFavouritesExpanded(): void {
    this.favouritesExpanded.update(v => !v);
  }

  async selectCollection(collection: Collection): Promise<void> {
    this.selectedCollection.set(collection);
    this.selectedFavouriteCollection.set(null);
    this.selectedRequest.set(null);
    this.selectedResponse.set(null);
    this.rightPanelMode.set('response');
    this.activeSection.set('collections');
    this.collectionsExpanded.set(true);
    this.requestSearchQuery.set('');
    try {
      await this.requestApi.loadForCollection(collection.id);
      const rememberedId = this.selectionState.getSelectedRequestForCollection(collection.id);
      if (rememberedId !== null) {
        const remembered = this.requests().find(r => r.id === rememberedId);
        if (remembered) {
          this.selectedRequest.set(remembered);
        }
      }
    } catch (err) {
      console.error(err);
    }
  }

  async selectFavouriteCollection(collection: FavouriteCollection): Promise<void> {
    this.selectedFavouriteCollection.set(collection);
    this.selectedCollection.set(null);
    this.selectedRequest.set(null);
    this.selectedResponse.set(null);
    this.rightPanelMode.set('response');
    this.activeSection.set('favourites');
    this.favouritesExpanded.set(true);
    this.requestSearchQuery.set('');
    try {
      await this.favouriteApi.loadRequestsForCollection(collection.id);
      const rememberedId = this.selectionState.getSelectedRequestForFavourite(collection.id);
      if (rememberedId !== null) {
        const remembered = this.favouriteRequests().find(r => r.id === rememberedId);
        if (remembered) {
          this.selectedRequest.set(remembered);
        }
      }
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

    const collection = this.selectedCollection();
    if (collection) {
      this.selectionState.setSelectedRequestForCollection(collection.id, req.id);
      return;
    }

    const favourite = this.selectedFavouriteCollection();
    if (favourite) {
      this.selectionState.setSelectedRequestForFavourite(favourite.id, req.id);
    }
  }

  openRequestContextMenu(req: HttpRequest, event: MouseEvent): void {
    event.preventDefault();
    this.requestContextMenuTarget.set(req);
    this.requestContextMenuX.set(event.clientX);
    this.requestContextMenuY.set(event.clientY);
    this.requestContextMenuOpen.set(true);
  }

  closeRequestContextMenu(): void {
    this.requestContextMenuOpen.set(false);
    this.requestContextMenuTarget.set(null);
  }

  async deleteRequest(): Promise<void> {
    const req = this.requestContextMenuTarget();
    if (!req) return;

    const collection = this.selectedCollection();
    const favourite = this.selectedFavouriteCollection();

    this.loading.set(true);
    try {
      if (collection) {
        await this.requestApi.delete(req.id);
        this.selectionState.deleteRequest(req.id);
      } else if (favourite) {
        await this.favouriteApi.removeRequest(favourite.id, req.id);
        this.selectionState.setSelectedRequestForFavourite(favourite.id, null);
      }

      if (this.selectedRequest()?.id === req.id) {
        this.selectedRequest.set(null);
        this.selectedResponse.set(null);
        this.rightPanelMode.set('response');
      }

      if (collection) {
        await this.requestApi.loadForCollection(collection.id);
      }
      if (favourite) {
        await this.favouriteApi.loadRequestsForCollection(favourite.id);
      }

      this.closeRequestContextMenu();
    } catch (err) {
      console.error(err);
      this.error.set(collection ? 'Failed to delete request.' : 'Failed to remove favourite.');
    } finally {
      this.loading.set(false);
    }
  }

  openCollectionContextMenu(collection: Collection, event: MouseEvent): void {
    event.preventDefault();
    this.collectionContextMenuTarget.set(collection);
    this.collectionContextMenuX.set(event.clientX);
    this.collectionContextMenuY.set(event.clientY);
    this.collectionContextMenuOpen.set(true);
  }

  closeCollectionContextMenu(): void {
    this.collectionContextMenuOpen.set(false);
    this.collectionContextMenuTarget.set(null);
  }

  editCollectionAppearance(): void {
    const collection = this.collectionContextMenuTarget();
    if (!collection) return;

    this.closeCollectionContextMenu();
    this.openCollectionAppearancePopup(collection);
  }

  openCollectionAppearancePopup(collection: Collection, event?: MouseEvent): void {
    event?.preventDefault();
    event?.stopPropagation();
    this.collectionAppearanceTarget.set(collection);
    this.collectionAppearanceTab.set('icon');
    this.collectionAppearancePopupOpen.set(true);
  }

  closeCollectionAppearancePopup(): void {
    this.collectionAppearancePopupOpen.set(false);
    this.collectionAppearanceTarget.set(null);
  }

  selectCollectionAppearanceColor(color: string): void {
    const collection = this.collectionAppearanceTarget();
    if (!collection) return;

    this.collectionAppearance.setColor(collection.id, color);
  }

  onAppearanceColorInput(event: Event): void {
    const value = (event.target as HTMLInputElement).value;
    this.selectCollectionAppearanceColor(value);
  }

  async selectCollectionAppearanceIcon(iconId: string): Promise<void> {
    const collection = this.collectionAppearanceTarget();
    if (!collection) return;

    try {
      const updated = await this.collectionApi.update({ ...collection, icon_id: iconId });
      this.collectionAppearanceTarget.set(updated);
    } catch (err) {
      console.error(err);
      this.error.set('Failed to save collection icon.');
    }
  }

  async resetCollectionAppearance(): Promise<void> {
    const collection = this.collectionAppearanceTarget();
    if (!collection) return;

    this.collectionAppearance.clearColor(collection.id);

    if (!collection.icon_id) return;

    try {
      const updated = await this.collectionApi.update({ ...collection, icon_id: '' });
      this.collectionAppearanceTarget.set(updated);
    } catch (err) {
      console.error(err);
      this.error.set('Failed to clear collection icon.');
    }
  }

  currentCollectionColor() {
    const collection = this.collectionAppearanceTarget();
    return collection ? this.collectionAppearance.colorFor(collection.id) : null;
  }

  async deleteCollection(): Promise<void> {
    const collection = this.collectionContextMenuTarget();
    if (!collection) return;

    this.loading.set(true);
    try {
      const deletedRequestIds = await this.collectionApi.delete(collection.id);
      for (const requestId of deletedRequestIds) {
        this.selectionState.deleteRequest(requestId);
      }
      this.selectionState.setSelectedRequestForCollection(collection.id, null);

      if (this.selectedCollection()?.id === collection.id) {
        this.selectedCollection.set(null);
        this.selectedRequest.set(null);
        this.selectedResponse.set(null);
        this.requestApi.requests.set([]);
        this.rightPanelMode.set('response');
      }

      this.closeCollectionContextMenu();
    } catch (err) {
      console.error(err);
      this.error.set('Failed to delete collection.');
    } finally {
      this.loading.set(false);
    }
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

      const favList = this.favouriteRequests();
      const favIndex = favList.findIndex(r => r.id === updated.id);
      if (favIndex !== -1) {
        const newFavList = [...favList];
        newFavList[favIndex] = updated;
        this.favouriteApi.requests.set(newFavList);
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
      if (this.selectedFavouriteCollection()) {
        await this.favouriteApi.loadRequestsForCollection(this.selectedFavouriteCollection()!.id);
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

  openFavouritePopup(req: HttpRequest, event: MouseEvent): void {
    event.stopPropagation();
    this.favouritePopupRequest.set(req);
    this.favouritePopupOpen.set(true);
    this.newFavouriteName.set('');
    this.favouriteApi.loadMembershipForRequest(req.id);
  }

  closeFavouritePopup(): void {
    this.favouritePopupOpen.set(false);
    this.favouritePopupRequest.set(null);
    this.favouriteApi.clearMembership();
  }

  openNewRequestPopup(): void {
    this.newRequestName.set('My new snappy API');
    this.newRequestUrl.set('');
    this.newRequestMethod.set('GET');
    this.newRequestPopupOpen.set(true);
  }

  closeNewRequestPopup(): void {
    this.newRequestPopupOpen.set(false);
  }

  async addRequest(): Promise<void> {
    const collection = this.selectedCollection();
    if (!collection) return;

    const name = this.newRequestName().trim();
    if (!name) return;

    this.loading.set(true);
    try {
      await this.requestApi.create({
        collection_id: collection.id,
        name,
        url: this.newRequestUrl().trim(),
        method: this.newRequestMethod(),
        body: '',
        request_headers: '',
        status_code: 0,
        response_id: 0,
      });
      await this.requestApi.loadForCollection(collection.id);
      this.closeNewRequestPopup();
    } catch (err) {
      console.error(err);
      this.error.set('Failed to add request.');
    } finally {
      this.loading.set(false);
    }
  }

  async toggleFavouriteMembership(collection: FavouriteCollection): Promise<void> {
    const req = this.favouritePopupRequest();
    if (!req) return;

    const isMember = this.favouriteMembership().has(collection.id);
    try {
      if (isMember) {
        await this.favouriteApi.removeRequest(collection.id, req.id);
      } else {
        await this.favouriteApi.addRequest(collection.id, req.id);
      }
      if (this.selectedFavouriteCollection()?.id === collection.id) {
        await this.favouriteApi.loadRequestsForCollection(collection.id);
      }
    } catch (err) {
      console.error(err);
      this.error.set('Failed to update favourites.');
    }
  }

  async addFavouriteCollection(): Promise<void> {
    const name = this.newFavouriteName().trim();
    const profile = this.selectedProject();
    if (!name || !profile) return;

    try {
      await this.favouriteApi.createCollection({ profile_id: profile.profile_id, name });
      this.newFavouriteName.set('');
    } catch (err) {
      console.error(err);
      this.error.set('Failed to add favourite collection.');
    }
  }

  async deleteFavouriteCollection(collection: FavouriteCollection, event: MouseEvent): Promise<void> {
    event.stopPropagation();
    try {
      await this.favouriteApi.deleteCollection(collection.id);
      if (this.selectedFavouriteCollection()?.id === collection.id) {
        this.selectedFavouriteCollection.set(null);
        this.favouriteApi.requests.set([]);
      }
    } catch (err) {
      console.error(err);
      this.error.set('Failed to delete favourite collection.');
    }
  }

  openNewCollectionPopup(): void {
    this.newCollectionName.set('');
    this.newCollectionPopupOpen.set(true);
  }

  closeNewCollectionPopup(): void {
    this.newCollectionPopupOpen.set(false);
  }

  async addCollection(): Promise<void> {
    const project = this.selectedProject();
    if (!project) return;

    const name = this.newCollectionName().trim();
    if (!name) return;

    this.loading.set(true);
    try {
      await this.collectionApi.create({
        project_id: project.id,
        name,
        icon_id: '',
      });
      await this.collectionApi.loadForProject(project.id);
      this.closeNewCollectionPopup();
    } catch (err) {
      console.error(err);
      this.error.set('Failed to add collection.');
    } finally {
      this.loading.set(false);
    }
  }

  openProjectEditor(): void {
    this.newProjectName.set('');
    this.projectEditorOpen.set(true);
    this.projectPendingDelete.set(null);
    this.projectDeleteConfirmOpen.set(false);
  }

  closeProjectEditor(): void {
    this.projectEditorOpen.set(false);
    this.projectPendingDelete.set(null);
    this.projectDeleteConfirmOpen.set(false);
  }

  async onSelectProject(project: Project): Promise<void> {
    if (this.selectedProject()?.id === project.id) {
      this.closeProjectEditor();
      return;
    }

    this.loading.set(true);
    try {
      await this.selectProject(project);
      this.closeProjectEditor();
    } catch (err) {
      console.error(err);
      this.error.set('Failed to switch project.');
    } finally {
      this.loading.set(false);
    }
  }

  async addProject(): Promise<void> {
    const profile = this.selectedProject();
    if (!profile) return;

    const name = this.newProjectName().trim();
    if (!name) return;

    this.loading.set(true);
    try {
      const created = await this.projectApi.create({
        profile_id: profile.profile_id,
        name,
      });
      await this.selectProject(created);
      this.newProjectName.set('');
    } catch (err) {
      console.error(err);
      this.error.set('Failed to add project.');
    } finally {
      this.loading.set(false);
    }
  }

  promptDeleteProject(project: Project, event: MouseEvent): void {
    event.stopPropagation();
    this.projectPendingDelete.set(project);
    this.projectDeleteConfirmOpen.set(true);
  }

  cancelDeleteProject(): void {
    this.projectPendingDelete.set(null);
    this.projectDeleteConfirmOpen.set(false);
  }

  async deleteProject(): Promise<void> {
    const project = this.projectPendingDelete();
    if (!project) return;

    this.loading.set(true);
    try {
      const deletedRequestIds = await this.projectApi.delete(project.id);
      for (const requestId of deletedRequestIds) {
        this.selectionState.deleteRequest(requestId);
      }

      if (this.selectedProject()?.id === project.id) {
        const remaining = this.projects().filter(p => p.id !== project.id);
        if (remaining.length > 0) {
          await this.selectProject(remaining[0]);
        } else {
          this.selectedProject.set(null);
          this.selectedEnvironment.set(null);
          this.selectedCollection.set(null);
          this.selectedFavouriteCollection.set(null);
          this.selectedRequest.set(null);
          this.selectedResponse.set(null);
          this.collectionApi.collections.set([]);
          this.requestApi.requests.set([]);
          this.favouriteApi.collections.set([]);
          this.favouriteApi.requests.set([]);
          this.environmentApi.environments.set([]);
        }
      }

      this.projectPendingDelete.set(null);
      this.projectDeleteConfirmOpen.set(false);
    } catch (err) {
      console.error(err);
      this.error.set('Failed to delete project.');
    } finally {
      this.loading.set(false);
    }
  }
}
