import { Component, input } from '@angular/core';

/** Marks the most recently used entry of a list. */
@Component({
  selector: 'app-last-used-paw',
  host: { class: 'last-used-paw', role: 'img', '[attr.aria-label]': 'hint()', '[title]': 'hint()' },
  template: `
    <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
      <ellipse cx="4.6" cy="10.2" rx="2.1" ry="2.6" transform="rotate(-20 4.6 10.2)" />
      <ellipse cx="9" cy="5.6" rx="2.2" ry="2.8" transform="rotate(-8 9 5.6)" />
      <ellipse cx="15" cy="5.6" rx="2.2" ry="2.8" transform="rotate(8 15 5.6)" />
      <ellipse cx="19.4" cy="10.2" rx="2.1" ry="2.6" transform="rotate(20 19.4 10.2)" />
      <path
        d="M12 11.2c-3.1 0-6.6 4.1-6.6 7.1 0 1.9 1.4 3 3.2 3 1.6 0 2.3-.9 3.4-.9s1.8.9 3.4.9c1.8 0 3.2-1.1 3.2-3 0-3-3.5-7.1-6.6-7.1z"
      />
    </svg>
  `,
  styles: `
    :host {
      display: inline-flex;
      flex: 0 0 auto;
      align-items: center;
      margin-left: auto;
      color: var(--muted);
    }

    svg {
      width: 14px;
      height: 14px;
    }
  `,
})
export class LastUsedPaw {
  readonly hint = input('Last used');
}
