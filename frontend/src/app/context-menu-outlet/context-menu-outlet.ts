import { Component, ElementRef, HostListener, afterRenderEffect, inject, OnDestroy, viewChild } from '@angular/core';
import { NgTemplateOutlet } from '@angular/common';
import { ContextMenuService, type ContextMenuPosition } from '../core/services/context-menu.service';

/** Closest a menu may come to the window edge. */
const EDGE_MARGIN = 8;

/**
 * Where to draw a menu of the given size opened at `at`, so it stays in the
 * window: like OS menus it opens upwards / to the left of the pointer when
 * there is no room below / to the right.
 */
function fitInWindow(at: ContextMenuPosition, width: number, height: number): ContextMenuPosition {
  const maxX = window.innerWidth - EDGE_MARGIN;
  const maxY = window.innerHeight - EDGE_MARGIN;
  const x = at.x + width > maxX ? at.x - width : at.x;
  const y = at.y + height > maxY ? at.y - height : at.y;
  // Still too big (tiny window): pin it inside the edges.
  return {
    x: Math.max(EDGE_MARGIN, Math.min(x, maxX - width)),
    y: Math.max(EDGE_MARGIN, Math.min(y, maxY - height)),
  };
}

@Component({
  selector: 'app-context-menu-outlet',
  imports: [NgTemplateOutlet],
  templateUrl: './context-menu-outlet.html',
  styleUrl: './context-menu-outlet.scss',
})
export class ContextMenuOutlet implements OnDestroy {
  protected readonly menu = inject(ContextMenuService);
  private readonly panel = viewChild<ElementRef<HTMLElement>>('panel');

  constructor() {
    // Position the menu once it is drawn, before the browser paints it: read
    // its size, then place it so it fits in the window.
    afterRenderEffect({
      earlyRead: () => {
        const el = this.panel()?.nativeElement;
        const at = this.menu.position();
        if (!el || !at) return null;
        const { width, height } = el.getBoundingClientRect();
        return { el, place: fitInWindow(at, width, height) };
      },
      write: measured => {
        const m = measured();
        if (!m) return;
        m.el.style.left = `${m.place.x}px`;
        m.el.style.top = `${m.place.y}px`;
      },
    });

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
