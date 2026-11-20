import { Component, inject, input, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { RequestApiService, type HttpRequest } from '../../core/services/request.service';
import { WorkspaceStateService } from '../../core/services/workspace-state.service';
import type { Collection } from '../../core/services/collection.service';
import { SelectOnFocusDirective } from '../../core/directives/select-on-focus.directive';

/**
 * Paste a cURL command and import it as a new request in the collection.
 * The caller then lets the user rename it, like a request made with "+ Request".
 */
@Component({
  selector: 'app-curl-import-dialog',
  imports: [FormsModule, SelectOnFocusDirective],
  templateUrl: './curl-import-dialog.html',
  styleUrl: './curl-import-dialog.scss',
  host: {
    '(document:keydown.escape)': 'closed.emit()',
  },
})
export class CurlImportDialog {
  private readonly requestApi = inject(RequestApiService);
  private readonly state = inject(WorkspaceStateService);

  /** The collection the imported request goes into. */
  readonly collection = input.required<Collection>();
  /** Emitted with the new request; the dialog closes itself right after. */
  readonly imported = output<HttpRequest>();
  readonly closed = output<void>();

  protected readonly curl = signal('');
  protected readonly error = signal('');

  protected async import(): Promise<void> {
    const curl = this.curl().trim();
    if (!curl) return;

    this.error.set('');
    this.state.loading.set(true);
    try {
      const created = await this.requestApi.createFromCurl(this.collection(), curl);
      this.imported.emit(created);
      this.closed.emit();
    } catch (err) {
      console.error('importing cURL failed', err);
      this.error.set("Couldn't read that cURL command. Check it and try again.");
    } finally {
      this.state.loading.set(false);
    }
  }
}
