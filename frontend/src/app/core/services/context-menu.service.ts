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

  open<T>(template: TemplateRef<{ $implicit: T }>, context: T, x: number, y: number): void {
    this.template.set(template as TemplateRef<{ $implicit: unknown }>);
    this.context.set(context);
    this.position.set({ x, y });
    this.isOpen.set(true);
  }

  close(): void {
    this.template.set(null);
    this.context.set(null);
    this.position.set(null);
    this.isOpen.set(false);
  }
}
