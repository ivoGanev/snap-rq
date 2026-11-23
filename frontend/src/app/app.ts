import {
  AfterViewInit,
  Component,
  DestroyRef,
  ElementRef,
  HostListener,
  inject,
  OnInit,
  signal,
  computed,
  effect,
  untracked,
  viewChild,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Select } from './select/select';
import { WML } from '@wailsio/runtime';
import { WorkspaceStateService } from './core/services/workspace-state.service';
import { ProjectApiService, type Project } from './core/services/project.service';
import { EnvironmentApiService, type Environment } from './core/services/environment.service';

import { CollectionApiService } from './core/services/collection.service';
import { RequestApiService } from './core/services/request.service';
import { FavouriteApiService } from './core/services/favourite.service';
import { SelectionStateService } from './core/services/selection-state.service';
import { ScriptApiService } from './core/services/script.service';
import { ThemeService, type Theme } from './core/services/theme.service';
import { SettingsApiService } from './core/services/settings.service';
import { IconManifestService } from './core/services/icon-manifest.service';
import { IconApiService } from './core/services/icon-api.service';
import { CommandApiService } from './core/services/command.service';
import { RequestGroups } from './request-groups/request-groups';
import { RequestsMainList } from './requests-main-list/requests-main-list';
import { ScriptEditor } from './script-editor/script-editor';
import { Inspector } from './inspector/inspector';
import { VariablesEditor } from './variables-editor/variables-editor';
import { CookiesEditor } from './cookies-editor/cookies-editor';
import { SettingsDialog, type SettingsCategory } from './settings-dialog/settings-dialog';
import { BackupApiService } from './core/services/backup.service';
import { RecoveryKeyDialog } from './dialogs/recovery-key-dialog/recovery-key-dialog';
import { SecretsApiService } from './core/services/secrets.service';
import { McpApprovalOutlet } from './mcp-approval-outlet/mcp-approval-outlet';
import { CookieApiService } from './core/services/cookie.service';
import { ZenMode } from './zen-mode/zen-mode';
import { ContextMenuOutlet } from './context-menu-outlet/context-menu-outlet';
import { ToastOutlet } from './toast-outlet/toast-outlet';
import { ConfirmOutlet } from './confirm-outlet/confirm-outlet';
import { OpenApiImportDialog } from './openapi-import-dialog/openapi-import-dialog';
import { OpenApiImportPrompt } from './core/services/openapi-import-prompt.service';
import { UndoPanel } from './undo-panel/undo-panel';

@Component({
  selector: 'app-root',
  imports: [
    FormsModule,
    Select,
    RequestGroups,
    RequestsMainList,
    ScriptEditor,
    Inspector,
    VariablesEditor,
    CookiesEditor,
    SettingsDialog,
    RecoveryKeyDialog,
    ZenMode,
    ContextMenuOutlet,
    ToastOutlet,
    ConfirmOutlet,
    OpenApiImportDialog,
    McpApprovalOutlet,
    UndoPanel,
  ],
  templateUrl: './app.html',
  styleUrl: './app.scss',
})
export class App implements OnInit, AfterViewInit {
  protected readonly state = inject(WorkspaceStateService);
  protected readonly theme = inject(ThemeService);
  protected readonly settingsApi = inject(SettingsApiService);
  protected readonly commands = inject(CommandApiService);
  protected readonly importPrompt = inject(OpenApiImportPrompt);
  private readonly projectApi = inject(ProjectApiService);
  private readonly environmentApi = inject(EnvironmentApiService);
  private readonly requestApi = inject(RequestApiService);
  private readonly collectionApi = inject(CollectionApiService);
  private readonly favouriteApi = inject(FavouriteApiService);
  private readonly selectionState = inject(SelectionStateService);
  private readonly scriptApi = inject(ScriptApiService);
  private readonly iconManifest = inject(IconManifestService);
  private readonly iconApi = inject(IconApiService);
  private readonly cookieApi = inject(CookieApiService);

  protected readonly projects = this.projectApi.projects;
  protected readonly environments = this.environmentApi.environments;
  protected readonly environmentOptions = computed(() =>
    this.environments().map((env) => ({ value: env.id, label: env.name })),
  );

  readonly leftColumnWidthExpanded = signal(22);
  readonly rightColumnWidth = signal(30);
  readonly resizingColumn = signal<'left' | 'right' | null>(null);

  /**
   * Collapsed rail is fixed; expanded without a group list shrinks to fit the
   * nav buttons; expanded with a list uses the resizable percenscripte.
   */
  readonly leftSidebarWidth = computed(() => {
    if (!this.state.sidebarExpanded()) return '72px';
    return this.state.sidebarListOpen() ? `${this.leftColumnWidthExpanded()}%` : 'max-content';
  });

  /**
   * The sidebar's actual width in px (collapsed, fitted to its buttons, or
   * resized): the top bar's logo cell matches it, so the drawer's line runs
   * up through the top bar and the environment lines up with the column
   * below.
   */
  protected readonly sidebarWidthPx = signal(72);
  private readonly sidebar = viewChild.required(RequestGroups, { read: ElementRef });
  private readonly destroyRef = inject(DestroyRef);

  readonly variablesOverlayOpen = signal(false);
  readonly cookiesOverlayOpen = signal(false);
  protected readonly cookieCount = this.cookieApi.cookies;

  /** Keeps the jar in step with the project; a new response may have set cookies. */
  private readonly cookieSync = effect(() => {
    const projectId = this.state.selectedProject()?.id ?? null;
    this.state.selectedResponse();
    void this.cookieApi.loadForProject(projectId);
  });
  readonly projectEditorOpen = signal(false);
  readonly newProjectName = signal('');
  readonly projectDeleteConfirmOpen = signal(false);
  readonly projectPendingDelete = signal<Project | null>(null);

  readonly themeSelectorOpen = signal(false);
  readonly settingsOpen = signal(false);
  /** The Settings page to open on (a backup toast's "Show" opens Backups). */
  readonly settingsCategory = signal<SettingsCategory>('general');
  /** The encryption key was replaced since the user saved a recovery key: announce the new one once. */
  readonly newRecoveryKeyOpen = signal(false);
  private readonly secretsApi = inject(SecretsApiService);
  private readonly backupApi = inject(BackupApiService);

  private resizeStartX = 0;
  private resizeStartWidth = 0;

  constructor() {
    // A "Backup saved" toast's "Show" opens Settings → Backups.
    effect(() => {
      if (this.backupApi.showRequested() > 0) untracked(() => this.openSettings('backups'));
    });
  }

  async ngOnInit(): Promise<void> {
    await this.cleanupDanglingIcons();
    await Promise.all([this.loadProjects(), this.settingsApi.load()]);
    void this.announceNewRecoveryKey();
  }

  private async announceNewRecoveryKey(): Promise<void> {
    try {
      if ((await this.secretsApi.recoveryKeyState()) === 'changed') this.newRecoveryKeyOpen.set(true);
    } catch (err) {
      console.error('checking the recovery key failed', err);
    }
  }

  ngAfterViewInit(): void {
    const sidebar = this.sidebar().nativeElement as HTMLElement;
    const observer = new ResizeObserver(() => this.sidebarWidthPx.set(sidebar.getBoundingClientRect().width));
    observer.observe(sidebar);
    this.destroyRef.onDestroy(() => observer.disconnect());

    WML.Enable();
  }

  /**
   * Resets stored icon appearances that reference ids no longer in the
   * registry, so removing an icon from the manifest also removes it from the
   * database. Runs before projects load so the UI sees cleaned data.
   */
  private async cleanupDanglingIcons(): Promise<void> {
    await this.iconManifest.ready;
    const ids = this.iconManifest.validIconIds();
    if (ids.length === 0) return;
    try {
      await this.iconApi.cleanupIconReferences(ids, this.iconManifest.defaultIconId());
    } catch (err) {
      console.error('icon cleanup failed', err);
    }
  }

  onEscapePressed(): void {
    if (this.state.zenModeOpen()) {
      this.state.closeZenMode();
      return;
    }
    if (this.cookiesOverlayOpen()) {
      this.cookiesOverlayOpen.set(false);
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

  openSettings(category: SettingsCategory = 'general'): void {
    this.settingsCategory.set(category);
    this.settingsOpen.set(true);
  }

  closeSettings(): void {
    this.settingsOpen.set(false);
  }

  // ---------- Undo / redo hotkeys ----------

  @HostListener('window:keydown.control.z', ['$event'])
  async onUndoHotkey(event: Event): Promise<void> {
    if (this.isEditableTarget(event)) {
      return; // let text editors run their own text-level undo
    }
    event.preventDefault();
    if (this.commands.undoSteps() === 0) return;
    try {
      await this.commands.undo();
    } catch (err) {
      console.error('Undo failed:', err);
    }
  }

  @HostListener('window:keydown.control.shift.z', ['$event'])
  @HostListener('window:keydown.control.y', ['$event'])
  async onRedoHotkey(event: Event): Promise<void> {
    if (this.isEditableTarget(event)) {
      return;
    }
    event.preventDefault();
    if (this.commands.redoSteps() === 0) return;
    try {
      await this.commands.redo();
    } catch (err) {
      console.error('Redo failed:', err);
    }
  }

  private isEditableTarget(event: Event): boolean {
    const target = event.target as HTMLElement | null;
    if (!target) return false;
    return !!target.closest('input, textarea, select, [contenteditable], .cm-editor');
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
        this.state.selectView(null);
        this.state.selectedRequest.set(null);
        this.state.selectedResponse.set(null);
        this.collectionApi.collections.set([]);
        this.requestApi.allRequests.set({});
        this.favouriteApi.collections.set([]);
        this.favouriteApi.requests.set([]);
        this.scriptApi.allScripts.set([]);
      }
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }

  async selectProject(project: Project): Promise<void> {
    this.state.selectedProject.set(project);
    this.state.selectViewForNewProject();
    this.state.selectedRequest.set(null);
    this.state.selectedResponse.set(null);
    this.requestApi.allRequests.set({});
    this.favouriteApi.requests.set([]);
    this.scriptApi.requestScripts.set({});

    await this.loadEnvironments(project.id);
    await this.favouriteApi.loadCollectionsForProject(project.id);
    await this.collectionApi.loadForProject(project.id);
    await this.scriptApi.loadScriptsForProject(project.id);
    await this.requestApi.loadAll();
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

  openCookiesOverlay(): void {
    void this.cookieApi.reload();
    this.cookiesOverlayOpen.set(true);
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
      await this.projectApi.delete(project.id);
      await this.projectApi.loadAll();

      if (this.state.selectedProject()?.id === project.id) {
        const remaining = this.projects();
        if (remaining.length > 0) {
          await this.selectProject(remaining[0]);
        } else {
          this.state.selectedProject.set(null);
          this.state.selectedEnvironment.set(null);
          this.state.selectView(null);
          this.state.selectedRequest.set(null);
          this.state.selectedResponse.set(null);
          this.collectionApi.collections.set([]);
          this.requestApi.allRequests.set({});
          this.favouriteApi.collections.set([]);
          this.favouriteApi.requests.set([]);
          this.scriptApi.allScripts.set([]);
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
