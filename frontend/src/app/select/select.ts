import { Component, ElementRef, computed, input, output, signal, viewChild } from '@angular/core';
import { CdkConnectedOverlay, CdkOverlayOrigin, type ConnectedPosition } from '@angular/cdk/overlay';

export interface SelectOption<T = string> {
  value: T;
  label: string;
  /** Options with the same group are listed under it, in the order given. */
  group?: string;
}

let nextId = 0;

/**
 * A dropdown drawn by the app. A native <select> opens the operating
 * system's own list, which CSS can only theme in Chromium.
 */
@Component({
  selector: 'app-select',
  imports: [CdkConnectedOverlay, CdkOverlayOrigin],
  templateUrl: './select.html',
  styleUrl: './select.scss',
})
export class Select<T = string> {
  readonly options = input.required<SelectOption<T>[]>();
  readonly value = input<T | null | undefined>(null);
  readonly placeholder = input('');
  readonly ariaLabel = input<string | null>(null, { alias: 'aria-label' });
  readonly valueChange = output<T>();

  protected readonly open = signal(false);
  protected readonly active = signal(-1);
  protected readonly listId = `app-select-${nextId++}`;
  private readonly trigger = viewChild.required<ElementRef<HTMLButtonElement>>('trigger');
  private readonly list = viewChild<ElementRef<HTMLElement>>('list');
  private typed = '';
  private typedAt = 0;

  protected readonly selectedIndex = computed(() => this.options().findIndex((o) => o.value === this.value()));
  protected readonly label = computed(() => this.options()[this.selectedIndex()]?.label ?? this.placeholder());

  protected readonly positions: ConnectedPosition[] = [
    { originX: 'start', originY: 'bottom', overlayX: 'start', overlayY: 'top', offsetY: 4 },
    { originX: 'start', originY: 'top', overlayX: 'start', overlayY: 'bottom', offsetY: -4 },
  ];

  protected startsGroup(index: number): string | null {
    const group = this.options()[index].group;
    if (!group) return null;
    return index === 0 || this.options()[index - 1].group !== group ? group : null;
  }

  protected optionId(index: number): string {
    return `${this.listId}-${index}`;
  }

  protected width(): number {
    return this.trigger().nativeElement.getBoundingClientRect().width;
  }

  // A list opening under a resting pointer gets pointer events without any
  // movement; only a real move takes the highlight from the keyboard.
  protected hover(index: number, event: PointerEvent): void {
    if (event.movementX || event.movementY) this.active.set(index);
  }

  protected toggle(): void {
    if (this.open()) this.close();
    else this.show();
  }

  private show(): void {
    if (!this.options().length) return;
    this.active.set(Math.max(this.selectedIndex(), 0));
    this.open.set(true);
  }

  protected close(): void {
    this.open.set(false);
  }

  protected choose(index: number): void {
    const option = this.options()[index];
    this.close();
    this.trigger().nativeElement.focus();
    if (option && option.value !== this.value()) this.valueChange.emit(option.value);
  }

  protected onKeydown(event: KeyboardEvent): void {
    const last = this.options().length - 1;
    const move = (to: () => number) => {
      event.preventDefault();
      if (!this.open()) {
        this.show();
        if (event.key === 'ArrowDown' || event.key === 'ArrowUp') return;
      }
      this.active.set(Math.min(Math.max(to(), 0), last));
      this.scrollActiveIntoView();
    };
    switch (event.key) {
      case 'ArrowDown':
        return move(() => this.active() + 1);
      case 'ArrowUp':
        return move(() => this.active() - 1);
      case 'Home':
        return move(() => 0);
      case 'End':
        return move(() => last);
      case 'Enter':
      case ' ':
        event.preventDefault();
        if (this.open()) this.choose(this.active());
        else this.show();
        return;
      case 'Escape':
      case 'Tab':
        if (this.open()) {
          if (event.key === 'Escape') event.preventDefault();
          this.close();
        }
        return;
    }
    if (event.key.length === 1 && !event.ctrlKey && !event.metaKey && !event.altKey) {
      this.typeAhead(event.key);
    }
  }

  // Typing letters jumps to the next option starting with them, as a native select does.
  private typeAhead(key: string): void {
    const now = Date.now();
    this.typed = now - this.typedAt > 700 ? key : this.typed + key;
    this.typedAt = now;
    const options = this.options();
    const from = this.open() ? this.active() : this.selectedIndex();
    for (let step = this.typed.length === 1 ? 1 : 0; step <= options.length; step++) {
      const index = (from + step + options.length) % options.length;
      if (options[index].label.toLowerCase().startsWith(this.typed.toLowerCase())) {
        if (this.open()) {
          this.active.set(index);
          this.scrollActiveIntoView();
        } else {
          this.choose(index);
        }
        return;
      }
    }
  }

  private scrollActiveIntoView(): void {
    queueMicrotask(() => {
      this.list()?.nativeElement.querySelector<HTMLElement>(`#${this.optionId(this.active())}`)
        ?.scrollIntoView({ block: 'nearest' });
    });
  }
}
