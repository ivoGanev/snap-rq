import { Component, inject } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { WorkspaceStateService } from '../core/services/workspace-state.service';
import { RequestEditor } from '../request-editor/request-editor';
import { ResponseViewer } from '../response-viewer/response-viewer';

@Component({
  selector: 'app-zen-mode',
  imports: [FormsModule, RequestEditor, ResponseViewer],
  templateUrl: './zen-mode.html',
  styleUrl: './zen-mode.scss',
  host: {
    class: 'zen-overlay',
    '(window:keydown.escape)': 'onEscapePressed()',
  },
})
export class ZenMode {
  protected readonly state = inject(WorkspaceStateService);

  onEscapePressed(): void {
    if (this.state.zenModeOpen()) {
      this.state.closeZenMode();
    }
  }

  close(): void {
    this.state.closeZenMode();
  }
}
