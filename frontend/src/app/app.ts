import { AfterViewInit, Component, inject, OnInit, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { WML } from '@wailsio/runtime';
import { WailsService } from './wails.service';
import { WorkspaceStateService } from './core/services/workspace-state.service';
import { ProjectApiService, type Project } from './core/services/project.service';
import { EnvironmentApiService, type Environment } from './core/services/environment.service';
import {
  EnvironmentVariableApiService,
  type EnvironmentVariable,
} from './core/services/environment-variable.service';
import { CollectionApiService } from './core/services/collection.service';
import { RequestApiService } from './core/services/request.service';
import { FavouriteApiService } from './core/services/favourite.service';
import { SelectionStateService } from './core/services/selection-state.service';
import { TagApiService } from './core/services/tag.service';
import { RequestGroups } from './request-groups/request-groups';
import { RequestsMainList } from './requests-main-list/requests-main-list';
import { RequestPanel } from './request-panel/request-panel';
import { ZenMode } from './zen-mode/zen-mode';
import * as EnvironmentService from '../../bindings/snap-rq/backend/services';

@Component({
  selector: 'app-root',
  imports: [FormsModule, RequestGroups, RequestsMainList, RequestPanel, ZenMode],
  templateUrl: './app.html',
  styleUrl: './app.scss',
})
export class App implements OnInit, AfterViewInit {
  protected readonly state = inject(WorkspaceStateService);
  private readonly wails = inject(WailsService);
  private readonly projectApi = inject(ProjectApiService);
  private readonly environmentApi = inject(EnvironmentApiService);
  private readonly variableApi = inject(EnvironmentVariableApiService);
  private readonly collectionApi = inject(CollectionApiService);
  private readonly requestApi = inject(RequestApiService);
  private readonly favouriteApi = inject(FavouriteApiService);
  private readonly selectionState = inject(SelectionStateService);
  private readonly tagApi = inject(TagApiService);

  protected readonly currentTime = this.wails.currentTime;
  protected readonly projects = this.projectApi.projects;
  protected readonly environments = this.environmentApi.environments;
  protected readonly variables = this.variableApi.variables;

  readonly leftColumnWidth = signal(220);
  readonly rightColumnWidth = signal(420);
  readonly resizingColumn = signal<'left' | 'right' | null>(null);

  readonly variablesOverlayOpen = signal(false);
  readonly projectEditorOpen = signal(false);
  readonly newProjectName = signal('');
  readonly projectDeleteConfirmOpen = signal(false);
  readonly projectPendingDelete = signal<Project | null>(null);

  private resizeStartX = 0;
  private resizeStartWidth = 0;

  async ngOnInit(): Promise<void> {
    await this.loadProjects();
  }

  ngAfterViewInit(): void {
    WML.Enable();
  }

  onEscapePressed(): void {
    if (this.state.zenModeOpen()) {
      this.state.closeZenMode();
      return;
    }
    if (this.variablesOverlayOpen()) {
      this.closeVariablesOverlay();
      return;
    }
    if (this.projectDeleteConfirmOpen()) {
      this.cancelDeleteProject();
      return;
    }
    if (this.projectEditorOpen()) {
      this.closeProjectEditor();
      return;
    }
  }

  startColumnResize(side: 'left' | 'right', event: MouseEvent): void {
    event.preventDefault();
    this.resizingColumn.set(side);
    this.resizeStartX = event.clientX;
    this.resizeStartWidth = side === 'left' ? this.leftColumnWidth() : this.rightColumnWidth();

    document.addEventListener('mousemove', this.onColumnResizeMove);
    document.addEventListener('mouseup', this.onColumnResizeEnd);
  }

  private readonly onColumnResizeMove = (event: MouseEvent): void => {
    const side = this.resizingColumn();
    if (!side) return;

    const delta = event.clientX - this.resizeStartX;
    const width = Math.max(160, this.resizeStartWidth + (side === 'left' ? delta : -delta));

    if (side === 'left') {
      this.leftColumnWidth.set(width);
    } else {
      this.rightColumnWidth.set(width);
    }
  };

  private readonly onColumnResizeEnd = (): void => {
    this.resizingColumn.set(null);
    document.removeEventListener('mousemove', this.onColumnResizeMove);
    document.removeEventListener('mouseup', this.onColumnResizeEnd);
  };

  async loadProjects(): Promise<void> {
    this.state.loading.set(true);
    try {
      await this.projectApi.loadAll();
      const all = this.projects();
      if (all.length > 0) {
        await this.selectProject(all[0]);
      } else {
        this.state.selectedProject.set(null);
        this.state.selectedEnvironment.set(null);
        this.state.selectedCollection.set(null);
        this.state.selectedFavouriteCollection.set(null);
        this.state.selectedRequest.set(null);
        this.state.selectedResponse.set(null);
        this.collectionApi.collections.set([]);
        this.requestApi.requests.set([]);
        this.favouriteApi.collections.set([]);
        this.favouriteApi.requests.set([]);
      }
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }

  async selectProject(project: Project): Promise<void> {
    this.state.selectedProject.set(project);
    this.state.selectedCollection.set(null);
    this.state.selectedFavouriteCollection.set(null);
    this.state.selectedTag.set(null);
    this.state.selectedRequest.set(null);
    this.state.selectedResponse.set(null);
    this.requestApi.requests.set([]);
    this.state.rightPanelMode.set('response');

    await this.loadEnvironments(project.id);
    await this.favouriteApi.loadCollectionsForProfile(project.profile_id);
    await this.collectionApi.loadForProject(project.id);
    await this.tagApi.loadAllTags();

    const collections = this.collectionApi.collections();
    if (collections.length > 0) {
      this.state.selectedCollection.set(collections[0]);
    }
  }

  async loadEnvironments(projectId: number): Promise<void> {
    try {
      await this.environmentApi.loadForProject(projectId);
      const all = this.environments();
      this.state.selectedEnvironment.set(all.length > 0 ? all[0] : null);
    } catch (err) {
      console.error(err);
    }
  }

  selectEnvironment(environment: Environment): void {
    this.state.selectedEnvironment.set(environment);
  }

  onEnvironmentChange(environmentId: number): void {
    const env = this.environments().find((e) => e.id === environmentId);
    if (env) {
      this.selectEnvironment(env);
    }
  }

  openVariablesOverlay(): void {
    this.variablesOverlayOpen.set(true);
    const env = this.state.selectedEnvironment();
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
    const index = list.findIndex((v) => v.id === updated.id);
    if (index !== -1) {
      const newList = [...list];
      newList[index] = updated;
      this.variableApi.variables.set(newList);
    }

    try {
      await this.variableApi.update(updated);
    } catch (err) {
      console.error(err);
    }
  }

  async addVariable(): Promise<void> {
    const env = this.state.selectedEnvironment();
    if (!env) return;

    try {
      const created = await this.variableApi.create({
        environment_id: env.id,
        key: 'NEW_KEY',
        value: '',
      });
      this.variableApi.variables.update((list) => [...list, created]);
    } catch (err) {
      console.error(err);
    }
  }

  async deleteVariable(variable: EnvironmentVariable, event: MouseEvent): Promise<void> {
    event.stopPropagation();
    try {
      await this.variableApi.delete(variable.id);
      this.variableApi.variables.update((list) => list.filter((v) => v.id !== variable.id));
    } catch (err) {
      console.error(err);
    }
  }

  async deleteEnvironment(): Promise<void> {
    const env = this.state.selectedEnvironment();
    const project = this.state.selectedProject();
    if (!env || !project) return;

    this.state.loading.set(true);
    try {
      await EnvironmentService.EnvironmentService.DeleteEnvironment(env.id);
      await this.loadEnvironments(project.id);
      this.closeVariablesOverlay();
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
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
    if (this.state.selectedProject()?.id === project.id) {
      this.closeProjectEditor();
      return;
    }

    this.state.loading.set(true);
    try {
      await this.selectProject(project);
      this.closeProjectEditor();
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }

  async addProject(): Promise<void> {
    const profile = this.state.selectedProject();
    if (!profile) return;

    const name = this.newProjectName().trim();
    if (!name) return;

    this.state.loading.set(true);
    try {
      const created = await this.projectApi.create({
        profile_id: profile.profile_id,
        name,
      });
      await this.selectProject(created);
      this.newProjectName.set('');
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
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

    this.state.loading.set(true);
    try {
      const deletedRequestIds = await this.projectApi.delete(project.id);
      for (const requestId of deletedRequestIds) {
        this.selectionState.deleteRequest(requestId);
      }

      if (this.state.selectedProject()?.id === project.id) {
        const remaining = this.projects().filter((p) => p.id !== project.id);
        if (remaining.length > 0) {
          await this.selectProject(remaining[0]);
        } else {
          this.state.selectedProject.set(null);
          this.state.selectedEnvironment.set(null);
          this.state.selectedCollection.set(null);
          this.state.selectedFavouriteCollection.set(null);
          this.state.selectedRequest.set(null);
          this.state.selectedResponse.set(null);
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
    } finally {
      this.state.loading.set(false);
    }
  }
}
