import { Component, computed, inject } from '@angular/core';
import { NgComponentOutlet } from '@angular/common';
import { InspectorService } from '../core/services/inspector.service';
import { INSPECTOR_PANES } from './inspector-panes';

/**
 * The right-hand panel. A plain container: it shows the pane registered for
 * the current Inspector content and passes the content in. It knows nothing
 * about requests, the trash bin or any other view; see InspectorService for
 * what is shown and INSPECTOR_PANES for how.
 */
@Component({
  selector: 'app-inspector',
  imports: [NgComponentOutlet],
  template: `<ng-container *ngComponentOutlet="pane(); inputs: { content: content() }" />`,
  host: {
    class: 'sidebar sidebar-right',
    'aria-label': 'Inspector',
  },
})
export class Inspector {
  protected readonly content = inject(InspectorService).content;
  protected readonly pane = computed(() => INSPECTOR_PANES[this.content().kind]);
}
