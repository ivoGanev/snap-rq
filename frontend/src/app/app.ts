import { AfterViewInit, Component, inject, OnInit, signal, computed } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { WML } from '@wailsio/runtime';
import { WorkspaceStateService } from './core/services/workspace-state.service';
import { ProjectApiService, type Project } from './core/services/project.service';
import { EnvironmentApiService, type Environment } from './core/services/environment.service';

import { CollectionApiService } from './core/services/collection.service';
import { RequestApiService } from './core/services/request.service';
import { ApiRequestsService } from './core/services/api-requests.service';
import { FavouriteApiService } from './core/services/favourite.service';
import { SelectionStateService } from './core/services/selection-state.service';
import { TagApiService } from './core/services/tag.service';
import { ThemeService, type Theme } from './core/services/theme.service';
import { SettingsApiService } from './core/services/settings.service';
import { IconManifestService } from './core/services/icon-manifest.service';
import { IconApiService } from './core/services/icon-api.service';
import { RequestGroups } from './request-groups/request-groups';
import { RequestsMainList } from './requests-main-list/requests-main-list';
import { RequestPanel } from './request-panel/request-panel';
import { VariablesEditor } from './variables-editor/variables-editor';
import { ZenMode } from './zen-mode/zen-mode';
import { ContextMenuOutlet } from './context-menu-outlet/context-menu-outlet';

@Component({
  selector: 'app-root',
  imports: [
    FormsModule,
    RequestGroups,
    RequestsMainList,
    RequestPanel,
    VariablesEditor,
    ZenMode,
    ContextMenuOutlet,
  ],
  templateUrl: './app.html',
  styleUrl: './app.scss',
})
export class App implements OnInit, AfterViewInit {
  protected readonly state = inject(WorkspaceStateService);
  protected readonly theme = inject(ThemeService);
  protected readonly settingsApi = inject(SettingsApiService);
  private readonly projectApi = inject(ProjectApiService);
  private readonly environmentApi = inject(EnvironmentApiService);
  private readonly apiRequests = inject(ApiRequestsService);
  private readonly collectionApi = inject(CollectionApiService);
  private readonly requestApi = inject(RequestApiService);
  private readonly favouriteApi = inject(FavouriteApiService);
  private readonly selectionState = inject(SelectionStateService);
  private readonly tagApi = inject(TagApiService);
  private readonly iconManifest = inject(IconManifestService);
  private readonly iconApi = inject(IconApiService);

  protected readonly projects = this.projectApi.projects;
  protected readonly environments = this.environmentApi.environments;

  readonly leftColumnWidthExpanded = signal(22);
  readonly rightColumnWidth = signal(30);
  readonly resizingColumn = signal<'left' | 'right' | null>(null);

  /**
   * Collapsed rail is fixed; expanded without a group list shrinks to fit the
   * nav buttons; expanded with a list uses the resizable percentage.
   */
  readonly leftSidebarWidth = computed(() => {
    if (!this.state.sidebarExpanded()) return '72px';
    return this.state.sidebarListOpen() ? `${this.leftColumnWidthExpanded()}%` : 'max-content';
  });

  readonly variablesOverlayOpen = signal(false);
  readonly projectEditorOpen = signal(false);
  readonly newProjectName = signal('');
  readonly projectDeleteConfirmOpen = signal(false);
  readonly projectPendingDelete = signal<Project | null>(null);

  readonly themeSelectorOpen = signal(false);
  readonly settingsOpen = signal(false);

  private resizeStartX = 0;
  private resizeStartWidth = 0;

  async ngOnInit(): Promise<void> {
    await Promise.all([this.loadProjects(), this.settingsApi.load()]);
    await this.migrateIconsIfNeeded();
  }

  ngAfterViewInit(): void {
    WML.Enable();
  }

  /**
   * One-time migration of stored icon ids. The icon manifest is the source of
   * truth for replacements; when any are declared, they are applied in a single
   * bulk backend call and the appearance-bearing lists are refreshed.
   */
  private async migrateIconsIfNeeded(): Promise<void> {
    await this.iconManifest.ready;
    const replacements = this.iconManifest.replacementPairs();
    if (replacements.length === 0) return;

    try {
      await this.iconApi.migrateIconIds(replacements);
    } catch (err) {
      console.error('icon migration failed', err);
      return;
    }

    const project = this.state.selectedProject();
    if (project) {
      await Promise.all([
        this.collectionApi.loadForProject(project.id),
        this.favouriteApi.loadCollectionsForProject(project.id),
        this.tagApi.loadTagsForProject(project.id),
      ]);
    }
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
    if (this.settingsOpen()) {
      this.closeSettings();
      return;
    }
    if (this.themeSelectorOpen()) {
      this.closeThemeSelector();
      return;
    }
  }

  openSettings(): void {
    this.settingsOpen.set(true);
  }

  closeSettings(): void {
    this.settingsOpen.set(false);
  }

  async onSslValidationChange(event: Event): Promise<void> {
    const checked = (event.target as HTMLInputElement).checked;
    try {
      await this.settingsApi.update({
        ...this.settingsApi.settings(),
        validate_ssl_certificates: checked,
      });
    } catch (err) {
      console.error('Failed to update settings:', err);
      this.settingsApi.settings.update((current) => ({ ...current }));
    }
  }

  async onRequestTimeoutChange(event: Event): Promise<void> {
    const input = event.target as HTMLInputElement;
    const parsed = Math.trunc(Number(input.value));
    const value = Number.isFinite(parsed) ? Math.min(99999, Math.max(1, parsed)) : NaN;
    const current = this.settingsApi.settings();
    if (Number.isNaN(value) || input.value.trim() === '') {
      input.value = String(current.request_timeout_seconds);
      return;
    }
    input.value = String(value);
    if (value === current.request_timeout_seconds) {
      return;
    }
    try {
      await this.settingsApi.update({ ...current, request_timeout_seconds: value });
    } catch (err) {
      console.error('Failed to update settings:', err);
      input.value = String(current.request_timeout_seconds);
    }
  }

  openThemeSelector(): void {
    this.themeSelectorOpen.set(true);
  }

  closeThemeSelector(): void {
    this.themeSelectorOpen.set(false);
  }

  selectTheme(theme: Theme): void {
    this.theme.select(theme.id);
    this.closeThemeSelector();
  }

  isActiveTheme(theme: Theme): boolean {
    return this.theme.currentTheme()?.id === theme.id;
  }

  startColumnResize(side: 'left' | 'right', event: MouseEvent): void {
    event.preventDefault();
    if (side === 'left' && !(this.state.sidebarExpanded() && this.state.sidebarListOpen())) {
      return;
    }
    this.resizingColumn.set(side);
    this.resizeStartX = event.clientX;
    this.resizeStartWidth =
      side === 'left' ? this.leftColumnWidthExpanded() : this.rightColumnWidth();

    document.addEventListener('mousemove', this.onColumnResizeMove);
    document.addEventListener('mouseup', this.onColumnResizeEnd);
  }

  private readonly onColumnResizeMove = (event: MouseEvent): void => {
    const side = this.resizingColumn();
    if (!side) return;

    const delta = event.clientX - this.resizeStartX;
    const deltaPercent = (delta / window.innerWidth) * 100;
    const width = Math.min(
      45,
      Math.max(12, this.resizeStartWidth + (side === 'left' ? deltaPercent : -deltaPercent)),
    );

    if (side === 'left') {
      this.leftColumnWidthExpanded.set(width);
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
        this.state.selectedTag.set(null);
        this.state.showingAllRequests.set(false);
        this.state.selectedRequest.set(null);
        this.state.selectedResponse.set(null);
        this.collectionApi.collections.set([]);
        this.requestApi.requests.set([]);
        this.favouriteApi.collections.set([]);
        this.favouriteApi.requests.set([]);
        this.tagApi.allTags.set([]);
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
    this.state.showingAllRequests.set(true);
    this.state.selectedRequest.set(null);
    this.state.selectedResponse.set(null);
    this.requestApi.requests.set([]);
    this.favouriteApi.requests.set([]);
    this.tagApi.requestTags.set({});

    await this.loadEnvironments(project.id);
    await this.favouriteApi.loadCollectionsForProject(project.id);
    await this.collectionApi.loadForProject(project.id);
    await this.tagApi.loadTagsForProject(project.id);
    await this.apiRequests.loadAll();
  }

  async loadEnvironments(projectId: number): Promise<void> {
    try {
      await this.environmentApi.loadForProject(projectId);
      let all = this.environments();
      if (all.length === 0) {
        const created = await this.environmentApi.create({
          project_id: projectId,
          name: 'Default',
          created_at: new Date().toISOString(),
        });
        all = this.environments();
        this.state.selectedEnvironment.set(created);
        return;
      }
      this.state.selectedEnvironment.set(all[0]);
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
  }

  closeVariablesOverlay(): void {
    this.variablesOverlayOpen.set(false);
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
          this.state.selectedTag.set(null);
          this.state.showingAllRequests.set(false);
          this.state.selectedRequest.set(null);
          this.state.selectedResponse.set(null);
          this.collectionApi.collections.set([]);
          this.requestApi.requests.set([]);
          this.favouriteApi.collections.set([]);
          this.favouriteApi.requests.set([]);
          this.tagApi.allTags.set([]);
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
