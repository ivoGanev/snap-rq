import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';
import { SIDEBAR_VIEWS, type SidebarViewId } from './sidebar-views';

/**
 * Collapsed sidebar: icon-only navigation with hover hints. The parent swaps
 * this in when no folder panel is open and swaps it out once a group list
 * needs to be shown.
 */
@Component({
  selector: 'app-sidebar-rail',
  templateUrl: './sidebar-rail.html',
  styleUrl: './sidebar-rail.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SidebarRail {
  readonly views = SIDEBAR_VIEWS;
  readonly activeView = input<SidebarViewId | null>(null);
  readonly disabled = input(false);
  readonly viewSelect = output<SidebarViewId>();
  readonly expand = output<void>();

  onViewClick(id: SidebarViewId): void {
    this.viewSelect.emit(id);
  }

  onExpandClick(): void {
    this.expand.emit();
  }
}
