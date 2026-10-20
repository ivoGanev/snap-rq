import { ChangeDetectionStrategy, Component } from '@angular/core';

/**
 * External-link arrow used by footer/documentation links.
 */
@Component({
  selector: 'app-external-link-icon',
  template: `
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
      stroke-linecap="round"
      stroke-linejoin="round"
      aria-hidden="true"
    >
      <line x1="7" y1="17" x2="17" y2="7" />
      <polyline points="7 7 17 7 17 17" />
    </svg>
  `,
  styles: `
    :host {
      display: inline-flex;
      align-items: center;
    }

    svg {
      width: 17px;
      height: 17px;
    }
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ExternalLinkIcon {}
