import { Component, computed, input } from '@angular/core';
import type { UsageLevel } from '../core/models/http';

const BARS: Record<UsageLevel, number> = { low: 1, medium: 2, high: 3 };

const LABELS: Record<UsageLevel, string> = {
  low: 'Low recent use',
  medium: 'Medium recent use',
  high: 'High recent use',
};

/**
 * How much a request is used lately, as three rising bars (like a signal
 * meter): high fills three, medium two, low one. Nothing at all for no
 * usage, so unused rows stay quiet. Not a count: the level fades with time.
 */
@Component({
  selector: 'app-usage-meter',
  template: `
    @if (level(); as level) {
      <span class="meter" role="img" [attr.aria-label]="label()" [title]="label()">
        @for (bar of [1, 2, 3]; track bar) {
          <span class="bar" [class.is-filled]="bar <= filled()"></span>
        }
      </span>
    }
  `,
  styles: `
    :host {
      display: inline-flex;
      align-items: center;
    }

    .meter {
      display: inline-flex;
      align-items: flex-end;
      gap: 2px;
      height: 14px;
    }

    .bar {
      width: 4px;
      border-radius: 1px;
      background: var(--glass-border);
    }

    .bar:nth-child(1) {
      height: 6px;
    }

    .bar:nth-child(2) {
      height: 10px;
    }

    .bar:nth-child(3) {
      height: 14px;
    }

    .bar.is-filled {
      background: var(--accent);
    }
  `,
})
export class UsageMeter {
  readonly level = input<UsageLevel | null>(null);

  protected readonly filled = computed(() => {
    const level = this.level();
    return level ? BARS[level] : 0;
  });
  protected readonly label = computed(() => {
    const level = this.level();
    return level ? LABELS[level] : '';
  });
}
