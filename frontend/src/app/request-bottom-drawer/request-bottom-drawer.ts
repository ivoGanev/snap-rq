import { ChangeDetectionStrategy, Component, computed, input, signal, type Type } from '@angular/core';
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
  readonly tabs = input<BottomTab[]>([]);
  readonly selectedIds = input<Set<number>>(new Set());
  readonly showingBin = input<boolean>(false);

  readonly activeTab = signal<BottomTab | null>(null);

  protected readonly tabInputs = computed(() => ({
    selectedIds: this.selectedIds(),
  }));

  selectTab(tab: BottomTab): void {
    this.activeTab.update((current) => (current?.id === tab.id ? null : tab));
  }

  closeDrawer(): void {
    this.activeTab.set(null);
  }

  isActive(tab: BottomTab): boolean {
    return this.activeTab()?.id === tab.id;
  }
}
