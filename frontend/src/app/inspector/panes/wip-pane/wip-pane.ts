import { Component, input } from '@angular/core';
import { Browser } from '@wailsio/runtime';
import { InspectorHeader } from '../../inspector-header/inspector-header';
import type { InspectorContentOf } from '../../inspector';

/** Placeholder until there is a real donation page (plan D4). */
const DONATE_URL = 'https://example.com/donate';

/**
 * Inspector pane for work in progress: scripts can't run yet. Centred note
 * with a link to support development.
 */
@Component({
  selector: 'app-wip-pane',
  imports: [InspectorHeader],
  templateUrl: './wip-pane.html',
  styleUrl: './wip-pane.scss',
})
export class WipPane {
  readonly content = input.required<InspectorContentOf<'wip'>>();

  protected readonly donateUrl = DONATE_URL;

  protected openDonate(event: MouseEvent): void {
    event.preventDefault();
    void Browser.OpenURL(DONATE_URL);
  }
}
