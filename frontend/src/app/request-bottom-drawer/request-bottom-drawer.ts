import { ChangeDetectionStrategy, Component, input, signal, type Type } from '@angular/core';
import { NgComponentOutlet } from '@angular/common';

export interface BottomTab {
  id: string;
  label: string;
  component: Type<unknown>;
}

/**
 * Generic bottom drawer for the request list.
 *
 * Renders a configurable tab bar and a sliding panel that hosts whichever tab
 * component is currently active. Hidden entirely when the bin is open.
 */
@Component({
  selector: 'app-request-bottom-drawer',
  imports: [NgComponentOutlet],
  templateUrl: './request-bottom-drawer.html',
  styleUrl: './request-bottom-drawer.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: {
    '(window:keydown.escape)': 'closeDrawer()',
  },
})
export class RequestBottomDrawer {
  private static readonly DEFAULT_HEIGHT = 400;
  private static readonly MIN_HEIGHT = 320;
  private static readonly MAX_HEIGHT_OFFSET = 80;

  readonly tabs = input<BottomTab[]>([]);
  readonly showingBin = input<boolean>(false);

  readonly activeTab = signal<BottomTab | null>(null);
  readonly drawerHeight = signal(RequestBottomDrawer.initialHeight());
  readonly resizing = signal(false);

  private resizeStartY = 0;
  private resizeStartHeight = this.drawerHeight();

  private static initialHeight(): number {
    return Math.max(RequestBottomDrawer.DEFAULT_HEIGHT, Math.round(window.innerHeight / 2));
  }

  selectTab(tab: BottomTab): void {
    this.activeTab.update((current) => (current?.id === tab.id ? null : tab));
  }

  closeDrawer(): void {
    this.activeTab.set(null);
  }

  isActive(tab: BottomTab): boolean {
    return this.activeTab()?.id === tab.id;
  }

  startResize(event: PointerEvent): void {
    event.preventDefault();
    this.resizeStartY = event.clientY;
    this.resizeStartHeight = this.drawerHeight();
    this.resizing.set(true);
    (event.currentTarget as HTMLElement).setPointerCapture(event.pointerId);
  }

  resize(event: PointerEvent): void {
    if (!this.resizing()) return;

    const maxHeight = Math.max(
      RequestBottomDrawer.MIN_HEIGHT,
      window.innerHeight - RequestBottomDrawer.MAX_HEIGHT_OFFSET,
    );
    const nextHeight = this.resizeStartHeight + this.resizeStartY - event.clientY;
    this.drawerHeight.set(
      Math.min(maxHeight, Math.max(RequestBottomDrawer.MIN_HEIGHT, nextHeight)),
    );
  }

  stopResize(event: PointerEvent): void {
    if (!this.resizing()) return;
    this.resizing.set(false);
    (event.currentTarget as HTMLElement).releasePointerCapture(event.pointerId);
  }
}
