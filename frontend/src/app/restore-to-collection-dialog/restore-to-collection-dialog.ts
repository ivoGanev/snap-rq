import { Component, inject, input, output } from '@angular/core';
import { BinApiService, type RestoreInput } from '../core/services/bin.service';
import { CollectionApiService, type Collection } from '../core/services/collection.service';
import { WorkspaceStateService } from '../core/services/workspace-state.service';

/**
 * Restore deleted requests when at least one of them lost its original
 * collection: the user picks where those go. Requests whose collection
 * still exists go back there.
 */
@Component({
  selector: 'app-restore-to-collection-dialog',
  templateUrl: './restore-to-collection-dialog.html',
  styleUrl: './restore-to-collection-dialog.scss',
  host: {
    '(document:keydown.escape)': 'closed.emit()',
  },
})
export class RestoreToCollectionDialog {
  private readonly binApi = inject(BinApiService);
  private readonly state = inject(WorkspaceStateService);
  protected readonly collections = inject(CollectionApiService).collections;

  /** Trash bin ids of the requests to restore. */
  readonly binIds = input.required<number[]>();
  /** Emitted after a successful restore, before `closed`. */
  readonly restored = output<void>();
  readonly closed = output<void>();

  protected async restoreTo(fallback: Collection): Promise<void> {
    const ids = new Set(this.binIds());
    const existing = new Set(this.collections().map(c => c.id));
    const inputs: RestoreInput[] = this.binApi
      .binnedRequests()
      .filter(r => ids.has(r.id))
      .map(r => ({
        bin_id: r.id,
        collection_id: existing.has(r.original_collection_id) ? r.original_collection_id : fallback.id,
      }));
    if (inputs.length === 0) return;

    this.state.loading.set(true);
    try {
      await this.binApi.restoreMany(inputs);
      this.state.selectedBinnedRequest.set(null);
      this.restored.emit();
      this.closed.emit();
    } catch (err) {
      console.error('restoring requests failed', err);
    } finally {
      this.state.loading.set(false);
    }
  }
}
