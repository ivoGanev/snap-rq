import { Injectable, signal, TemplateRef } from '@angular/core';

export interface ContextMenuPosition {
  x: number;
  y: number;
}

export interface ContextMenuState<T = unknown> {
  template: TemplateRef<{ $implicit: T }> | null;
  context: T | null;
  position: ContextMenuPosition | null;
}

/**
 * Global context-menu controller.
 *
 * The menu itself is rendered by ContextMenuOutletComponent at the root of the
 * app so it is never clipped by the flex columns. Any component can open a menu
 * by passing an ng-template and the context data (usually the right-clicked
 * item).
 */
@Injectable({ providedIn: 'root' })
export class ContextMenuService {
  private readonly state = signal<ContextMenuState>({
    template: null,
    context: null,
    position: null,
  });

  readonly template = signal<TemplateRef<{ $implicit: unknown }> | null>(null);
  readonly context = signal<unknown>(null);
  readonly position = signal<ContextMenuPosition | null>(null);
  readonly isOpen = signal(false);
  /** Opened by hovering: closes once the pointer has left its button and the menu. */
  private openedByHover = false;
  private leaveTimer: ReturnType<typeof setTimeout> | undefined;

  open<T>(template: TemplateRef<{ $implicit: T }>, context: T, x: number, y: number, byHover = false): void {
    clearTimeout(this.leaveTimer);
    this.openedByHover = byHover;
    this.template.set(template as TemplateRef<{ $implicit: unknown }>);
    this.context.set(context);
    this.position.set({ x, y });
    this.isOpen.set(true);
  }

  /** The pointer is over a hover menu or its button. */
  pointerEntered(): void {
    clearTimeout(this.leaveTimer);
  }

  /** The pointer left a hover menu or its button; a short grace lets it cross between the two. */
  pointerLeft(): void {
    if (!this.openedByHover) return;
    clearTimeout(this.leaveTimer);
    this.leaveTimer = setTimeout(() => this.close(), 150);
  }

  close(): void {
    clearTimeout(this.leaveTimer);
    this.openedByHover = false;
    this.template.set(null);
    this.context.set(null);
    this.position.set(null);
    this.isOpen.set(false);
  }
}
