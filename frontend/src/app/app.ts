import {
  AfterViewInit,
  Component,
  HostListener,
  inject,
  OnInit,
  signal,
  computed,
  effect,
  untracked,
  ElementRef,
  Injector,
  afterNextRender,
  viewChild,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { CdkConnectedOverlay, CdkOverlayOrigin, type ConnectedPosition } from '@angular/cdk/overlay';
import { SelectOnFocusDirective } from './core/directives/select-on-focus.directive';
import { Select } from './select/select';
import { WindowControls } from './window-controls/window-controls';
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
import { UpdateCheckService } from './core/services/update-check.service';
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
import { BinApiService } from './core/services/bin.service';
import { ConfirmService } from './core/services/confirm.service';
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
    CdkConnectedOverlay,
    CdkOverlayOrigin,
    SelectOnFocusDirective,
    Select,
    WindowControls,
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
  protected readonly binApi = inject(BinApiService);
  private readonly confirm = inject(ConfirmService);

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

  readonly variablesOverlayOpen = signal(false);
  readonly cookiesOverlayOpen = signal(false);
  protected readonly cookieCount = this.cookieApi.cookies;
  protected readonly isWindows = document.documentElement.classList.contains('platform-windows');
  protected readonly isMac = document.documentElement.classList.contains('platform-mac');

  async emptyBin(): Promise<void> {
    const ids = this.binApi.binnedRequests().map((r) => r.id);
    if (ids.length === 0) return;
    const confirmed = await this.confirm.ask({
      title: 'Empty the trash bin?',
      message:
        ids.length === 1
          ? 'The request in the bin will be permanently deleted. This cannot be undone.'
          : `All ${ids.length} requests in the bin will be permanently deleted. This cannot be undone.`,
      confirmLabel: 'Empty bin',
      danger: true,
    });
    if (!confirmed) return;
    try {
      await this.binApi.deleteManyPermanently(ids);
      this.state.selectedBinnedRequest.set(null);
      this.state.selectedIds.set(new Set());
      this.state.multiSelectionActive.set(false);
    } catch (err) {
      console.error(err);
    }
  }
  protected readonly searchOpen = signal(false);
  private readonly searchInput = viewChild<ElementRef<HTMLInputElement>>('searchInput');
  private readonly injector = inject(Injector);

  /** Keeps the jar in step with the project; a new response may have set cookies. */
  private readonly cookieSync = effect(() => {
    const projectId = this.state.selectedProject()?.id ?? null;
    this.state.selectedResponse();
    void this.cookieApi.loadForProject(projectId);
  });
  readonly projectMenuOpen = signal(false);
  readonly projectAdding = signal(false);
  readonly newProjectName = signal('');
  readonly projectPendingDelete = signal<Project | null>(null);
  /** What's typed to confirm a delete: "ok", in any case. */
  readonly deleteConfirmName = signal('');
  protected readonly deleteConfirmMatches = computed(() => this.deleteConfirmName().trim().toLowerCase() === 'ok');
  readonly projectRenaming = signal<Project | null>(null);
  readonly renameProjectValue = signal('');
  protected readonly projectMenuPositions: ConnectedPosition[] = [
    { originX: 'start', originY: 'bottom', overlayX: 'start', overlayY: 'top', offsetY: 6 },
  ];

  readonly themeSelectorOpen = signal(false);
  readonly settingsOpen = signal(false);
  /** The Settings page to open on (a backup toast's "Show" opens Backups). */
  readonly settingsCategory = signal<SettingsCategory>('general');
  /** The encryption key was replaced since the user saved a recovery key: announce the new one once. */
  readonly newRecoveryKeyOpen = signal(false);
  private readonly secretsApi = inject(SecretsApiService);
  private readonly backupApi = inject(BackupApiService);
  protected readonly updates = inject(UpdateCheckService);

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
    if (this.projectMenuOpen()) {
      this.closeProjectMenu();
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

  @HostListener('window:keydown.control.f', ['$event'])
  @HostListener('window:keydown.meta.f', ['$event'])
  onSearchHotkey(event: Event): void {
    event.preventDefault();
    this.openSearch();
  }

  protected toggleSearch(): void {
    if (this.searchOpen()) this.closeSearch();
    else this.openSearch();
  }

  private openSearch(): void {
    this.searchOpen.set(true);
    afterNextRender(() => this.searchInput()?.nativeElement.focus(), { injector: this.injector });
  }

  protected closeSearch(): void {
    this.state.requestSearchQuery.set('');
    this.searchOpen.set(false);
  }

  // An empty field folds away; a search in use stays open.
  protected onSearchBlur(): void {
    if (!this.state.requestSearchQuery().trim()) this.searchOpen.set(false);
  }

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

  /** Where the theme popup opens: under its button, right-aligned with it. */
  protected readonly themePopupAt = signal({ top: 0, right: 0 });

  openThemeSelector(event: MouseEvent): void {
    const button = (event.currentTarget as HTMLElement).getBoundingClientRect();
    this.themePopupAt.set({ top: button.bottom + 6, right: window.innerWidth - button.right });
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
        await this.state.selectEnvironment(created);
        return;
      }
      const stored = this.projects().find((p) => p.id === projectId)?.selected_environment_id;
      const storedEnvironment = all.find((e) => e.id === stored);
      if (storedEnvironment) {
        this.state.selectedEnvironment.set(storedEnvironment);
      } else {
        await this.state.selectEnvironment(all[0]);
      }
    } catch (err) {
      console.error(err);
    }
  }

  selectEnvironment(environment: Environment): void {
    void this.state.selectEnvironment(environment);
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

  toggleSidebar(): void {
    this.state.sidebarExpanded.update((expanded) => !expanded);
  }

  toggleProjectMenu(): void {
    if (this.projectMenuOpen()) {
      this.closeProjectMenu();
    } else {
      this.projectMenuOpen.set(true);
    }
  }

  closeProjectMenu(): void {
    this.projectMenuOpen.set(false);
    this.cancelAddProject();
    this.cancelDeleteProject();
    this.cancelRenameProject();
  }

  /** From the empty workspace: the dropdown, ready to name a project. */
  openNewProject(): void {
    this.projectMenuOpen.set(true);
    this.startAddProject();
  }

  startAddProject(): void {
    this.cancelDeleteProject();
    this.cancelRenameProject();
    this.newProjectName.set('');
    this.projectAdding.set(true);
  }

  cancelAddProject(): void {
    this.projectAdding.set(false);
    this.newProjectName.set('');
  }

  async onSelectProject(project: Project): Promise<void> {
    if (this.state.selectedProject()?.id === project.id) {
      this.closeProjectMenu();
      return;
    }

    this.state.loading.set(true);
    try {
      await this.selectProject(project);
      this.closeProjectMenu();
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }

  async addProject(): Promise<void> {
    const name = this.newProjectName().trim();
    if (!name) return;

    this.state.loading.set(true);
    try {
      // With no project open, the profile is looked up rather than borrowed from one.
      const profileId = this.state.selectedProject()?.profile_id ?? (await this.projectApi.profileId());
      if (profileId === null) return;
      const created = await this.projectApi.create({ profile_id: profileId, name });
      await this.selectProject(created);
      this.closeProjectMenu();
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }

  startRenameProject(project: Project): void {
    this.cancelAddProject();
    this.cancelDeleteProject();
    this.renameProjectValue.set(project.name);
    this.projectRenaming.set(project);
  }

  cancelRenameProject(): void {
    this.projectRenaming.set(null);
    this.renameProjectValue.set('');
  }

  async saveRenameProject(): Promise<void> {
    const project = this.projectRenaming();
    const name = this.renameProjectValue().trim();
    if (!project || !name) return;
    if (name === project.name) {
      this.cancelRenameProject();
      return;
    }
    try {
      await this.projectApi.rename(project, name);
      if (this.state.selectedProject()?.id === project.id) {
        this.state.selectedProject.set({ ...project, name });
      }
      this.cancelRenameProject();
    } catch (err) {
      console.error(err);
    }
  }

  promptDeleteProject(project: Project): void {
    this.cancelAddProject();
    this.cancelRenameProject();
    this.deleteConfirmName.set('');
    this.projectPendingDelete.set(project);
  }

  cancelDeleteProject(): void {
    this.projectPendingDelete.set(null);
    this.deleteConfirmName.set('');
  }

  async deleteProject(): Promise<void> {
    const project = this.projectPendingDelete();
    if (!project || !this.deleteConfirmMatches()) return;

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

      this.cancelDeleteProject();
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }
}
