import { Component, inject, input, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { RequestApiService } from '../core/services/request.service';
import { WorkspaceStateService } from '../core/services/workspace-state.service';
import type { Collection } from '../core/services/collection.service';
import { HTTP_METHODS } from '../core/models/http';

type Mode = 'manual' | 'curl';

const DEFAULT_NAME = 'My new snappy API';

/**
 * Add a request to a collection: fill in name, URL and method, or paste a
 * cURL command to import.
 */
@Component({
  selector: 'app-new-request-dialog',
  imports: [FormsModule],
  templateUrl: './new-request-dialog.html',
  styleUrl: './new-request-dialog.scss',
  host: {
    '(document:keydown.escape)': 'closed.emit()',
  },
})
export class NewRequestDialog {
  private readonly requestApi = inject(RequestApiService);
  private readonly state = inject(WorkspaceStateService);

  /** The collection the new request goes into. */
  readonly collection = input.required<Collection>();
  readonly closed = output<void>();

  protected readonly httpMethods = HTTP_METHODS;
  protected readonly mode = signal<Mode>('manual');
  protected readonly name = signal(DEFAULT_NAME);
  protected readonly url = signal('');
  protected readonly method = signal('GET');
  protected readonly curl = signal('');

  protected setMode(mode: Mode): void {
    // An imported request takes its name from the cURL command unless one is typed.
    this.name.set(mode === 'curl' ? '' : DEFAULT_NAME);
    this.mode.set(mode);
  }

  protected async add(): Promise<void> {
    if (this.mode() === 'curl') {
      await this.addFromCurl();
      return;
    }

    const name = this.name().trim();
    if (!name) return;
    const collection = this.collection();

    await this.create(async () => ({
      collection_id: collection.id,
      project_id: collection.project_id,
      name,
      url: this.url().trim(),
      method: this.method(),
      body: '',
      body_type: 'json',
      body_form: '',
      request_headers: '',
      status_code: 0,
      response_id: 0,
    }));
  }

  private async addFromCurl(): Promise<void> {
    const curl = this.curl().trim();
    if (!curl) return;
    const collection = this.collection();

    await this.create(async () => {
      const req = await this.requestApi.curlToRequest(collection.id, curl);
      return {
        collection_id: collection.id,
        project_id: collection.project_id,
        name: this.name().trim() || req.name,
        url: req.url,
        method: req.method,
        body: req.body,
        body_type: req.body_type || 'json',
        body_form: req.body_form || '',
        request_headers: req.request_headers,
        status_code: 0,
        response_id: 0,
      };
    });
  }

  /** Builds the request, creates it and closes; errors leave the dialog open. */
  private async create(build: () => Promise<Parameters<RequestApiService['create']>[0]>): Promise<void> {
    this.state.loading.set(true);
    try {
      await this.requestApi.create(await build());
      this.closed.emit();
    } catch (err) {
      console.error('creating request failed', err);
    } finally {
      this.state.loading.set(false);
    }
  }
}
