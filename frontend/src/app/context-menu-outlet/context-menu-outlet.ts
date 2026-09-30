import { Component, HostListener, inject, OnDestroy } from '@angular/core';
import { NgTemplateOutlet } from '@angular/common';
import { ContextMenuService } from '../core/services/context-menu.service';

@Component({
  selector: 'app-context-menu-outlet',
  imports: [NgTemplateOutlet],
  templateUrl: './context-menu-outlet.html',
  styleUrl: './context-menu-outlet.scss',
})
export class ContextMenuOutlet implements OnDestroy {
  protected readonly menu = inject(ContextMenuService);

  constructor() {
    // Close on left-click outside the menu (bubble phase; menu clicks stop propagation).
    document.addEventListener('click', this.close, false);
    // Close on right-click outside the menu *before* the target's own contextmenu
    // handler fires, so the target can immediately open its own menu.
    document.addEventListener('contextmenu', this.close, true);
  }

  ngOnDestroy(): void {
    document.removeEventListener('click', this.close, false);
    document.removeEventListener('contextmenu', this.close, true);
  }

  @HostListener('document:keydown.escape')
  onEscape(): void {
    this.menu.close();
  }

  protected onMenuClick(event: MouseEvent): void {
    event.stopPropagation();
  }

  protected onMenuContextMenu(event: MouseEvent): void {
    event.preventDefault();
    event.stopPropagation();
  }

  private readonly close = (): void => {
    this.menu.close();
  };
}
