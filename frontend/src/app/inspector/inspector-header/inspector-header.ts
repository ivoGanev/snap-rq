import { Component, input } from '@angular/core';

/**
 * Header bar shared by every Inspector pane, so they all look the same.
 * Anything projected into it (e.g. a count) is shown after the title.
 */
@Component({
  selector: 'app-inspector-header',
  template: `
    <div class="column-header tab-header">
      <span class="panel-title">{{ title() }}</span>
      <ng-content />
    </div>
  `,
  styleUrl: './inspector-header.scss',
})
export class InspectorHeader {
  readonly title = input('');
}
