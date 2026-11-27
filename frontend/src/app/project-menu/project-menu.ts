import { Component, computed, inject, output, signal } from '@angular/core';
import { CdkConnectedOverlay, CdkOverlayOrigin, type ConnectedPosition } from '@angular/cdk/overlay';
import { ProjectApiService, type Project } from '../core/services/project.service';
import { WorkspaceStateService } from '../core/services/workspace-state.service';
import { SelectOnFocusDirective } from '../core/directives/select-on-focus.directive';

/**
 * The project name in the top bar and its dropdown: switch, create, rename
 * and delete projects. Opening a project reloads the workspace, which the
 * app does on (select); it also tidies up on (deleted).
 */
@Component({
  selector: 'app-project-menu',
  imports: [CdkConnectedOverlay, CdkOverlayOrigin, SelectOnFocusDirective],
  templateUrl: './project-menu.html',
  styleUrl: './project-menu.scss',
})
export class ProjectMenu {
  protected readonly state = inject(WorkspaceStateService);
  private readonly projectApi = inject(ProjectApiService);

  /** A project to open: picked, or just created. */
  readonly select = output<Project>();
  readonly deleted = output<Project>();

  protected readonly projects = this.projectApi.projects;
  protected readonly open = signal(false);
  protected readonly adding = signal(false);
  protected readonly newName = signal('');
  protected readonly renaming = signal<Project | null>(null);
  protected readonly renameValue = signal('');
  protected readonly pendingDelete = signal<Project | null>(null);
  /** What's typed to confirm a delete: "ok", in any case. */
  protected readonly confirmText = signal('');
  protected readonly confirmed = computed(() => this.confirmText().trim().toLowerCase() === 'ok');
  /** The delete note, shown at once on the info icon; title tooltips wait a second. */
  protected readonly hintOpen = signal(false);

  protected readonly menuPositions: ConnectedPosition[] = [
    { originX: 'start', originY: 'bottom', overlayX: 'start', overlayY: 'top', offsetY: 6 },
  ];
  protected readonly hintPositions: ConnectedPosition[] = [
    { originX: 'start', originY: 'bottom', overlayX: 'start', overlayY: 'top', offsetY: 6 },
    { originX: 'start', originY: 'top', overlayX: 'start', overlayY: 'bottom', offsetY: -6 },
  ];

  /** Opens the dropdown ready to name a new project. */
  openNew(): void {
    this.open.set(true);
    this.startAdd();
  }

  protected toggle(): void {
    if (this.open()) {
      this.close();
    } else {
      this.open.set(true);
    }
  }

  protected close(): void {
    this.open.set(false);
    this.cancelAdd();
    this.cancelRename();
    this.cancelDelete();
  }

  // Fields handle their own Esc; this one closes the dropdown, and stops the
  // app's Esc handling from acting on what's behind it.
  protected onKeydown(event: KeyboardEvent): void {
    if (event.key !== 'Escape') return;
    event.stopPropagation();
    this.close();
  }

  protected pick(project: Project): void {
    if (this.state.selectedProject()?.id !== project.id) this.select.emit(project);
    this.close();
  }

  protected startAdd(): void {
    this.cancelRename();
    this.cancelDelete();
    this.newName.set('');
    this.adding.set(true);
  }

  protected cancelAdd(): void {
    this.adding.set(false);
    this.newName.set('');
  }

  protected async add(): Promise<void> {
    const name = this.newName().trim();
    if (!name) return;
    try {
      // With no project open, the profile is looked up rather than borrowed from one.
      const profileId = this.state.selectedProject()?.profile_id ?? (await this.projectApi.profileId());
      if (profileId === null) return;
      const created = await this.projectApi.create({ profile_id: profileId, name });
      this.select.emit(created);
      this.close();
    } catch (err) {
      console.error(err);
    }
  }

  protected startRename(project: Project): void {
    this.cancelAdd();
    this.cancelDelete();
    this.renameValue.set(project.name);
    this.renaming.set(project);
  }

  protected cancelRename(): void {
    this.renaming.set(null);
    this.renameValue.set('');
  }

  protected async saveRename(): Promise<void> {
    const project = this.renaming();
    const name = this.renameValue().trim();
    if (!project || !name) return;
    if (name === project.name) {
      this.cancelRename();
      return;
    }
    try {
      await this.projectApi.rename(project, name);
      if (this.state.selectedProject()?.id === project.id) {
        this.state.selectedProject.set({ ...project, name });
      }
      this.cancelRename();
    } catch (err) {
      console.error(err);
    }
  }

  protected askDelete(project: Project): void {
    this.cancelAdd();
    this.cancelRename();
    this.confirmText.set('');
    this.pendingDelete.set(project);
  }

  protected cancelDelete(): void {
    this.hintOpen.set(false);
    this.pendingDelete.set(null);
    this.confirmText.set('');
  }

  protected async delete(): Promise<void> {
    const project = this.pendingDelete();
    if (!project || !this.confirmed()) return;
    try {
      await this.projectApi.delete(project.id);
      await this.projectApi.loadAll();
      this.cancelDelete();
      this.deleted.emit(project);
    } catch (err) {
      console.error(err);
    }
  }
}
