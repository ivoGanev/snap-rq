import { Component, input } from '@angular/core';
import { InspectorHeader } from '../../inspector-header/inspector-header';
import type { InspectorContentOf } from '../../inspector';

/** Inspector pane that only shows a message (empty and multi-selection states). */
@Component({
  selector: 'app-message-pane',
  imports: [InspectorHeader],
  template: `
    <app-inspector-header />
    @if (content().text) {
      <div class="state-message">{{ content().text }}</div>
    }
  `,
  styles: `
    :host {
      display: contents;
    }
  `,
})
export class MessagePane {
  readonly content = input.required<InspectorContentOf<'message'>>();
}
