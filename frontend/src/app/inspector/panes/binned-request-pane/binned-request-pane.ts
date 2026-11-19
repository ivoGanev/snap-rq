import { Component, computed, inject, input, signal } from '@angular/core';
import { InspectorHeader } from '../../inspector-header/inspector-header';
import type { InspectorContentOf } from '../../inspector';
import { WorkspaceStateService } from '../../../core/services/workspace-state.service';
import { BinApiService, type RestoreInput } from '../../../core/services/bin.service';
import { CollectionApiService, type Collection } from '../../../core/services/collection.service';

/**
 * Inspector pane for a request in the trash bin: shows where it came from and
 * lets the user restore it (to its original collection, or a picked one when
 * that collection is gone) or delete it permanently.
 */
@Component({
  selector: 'app-binned-request-pane',
  imports: [InspectorHeader],
  templateUrl: './binned-request-pane.html',
  styleUrl: './binned-request-pane.scss',
})
export class BinnedRequestPane {
  private readonly state = inject(WorkspaceStateService);
  private readonly binApi = inject(BinApiService);
  protected readonly collectionApi = inject(CollectionApiService);

  readonly content = input.required<InspectorContentOf<'binned-request'>>();

  protected readonly request = computed(
    () => this.binApi.binnedRequests().find(r => r.id === this.content().binId) ?? null,
  );
  protected readonly restoreCollectionModalOpen = signal(false);

  openRestorePicker(): void {
    const req = this.request();
    if (!req) return;

    const collections = this.collectionApi.collections();
    const originalExists = collections.some(c => c.id === req.original_collection_id);

    if (originalExists) {
      void this.doRestore([{ bin_id: req.id, collection_id: req.original_collection_id }]);
      return;
    }

    this.restoreCollectionModalOpen.set(true);
  }

  closeRestoreCollectionModal(): void {
    this.restoreCollectionModalOpen.set(false);
  }

  async restoreToCollection(collection: Collection): Promise<void> {
    const req = this.request();
    if (!req) return;

    await this.doRestore([{ bin_id: req.id, collection_id: collection.id }]);
    this.closeRestoreCollectionModal();
  }

  private async doRestore(inputs: RestoreInput[]): Promise<void> {
    if (inputs.length === 0) return;

    this.state.loading.set(true);
    try {
      await this.binApi.restoreMany(inputs);
      this.state.selectedBinnedRequest.set(null);
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }

  async deletePermanently(): Promise<void> {
    const req = this.request();
    if (!req) return;

    this.state.loading.set(true);
    try {
      await this.binApi.deletePermanently(req.id);
      this.state.selectedBinnedRequest.set(null);
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }
}
