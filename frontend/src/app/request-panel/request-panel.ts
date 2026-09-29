import { Component, effect, inject, signal } from '@angular/core';
import { WorkspaceStateService } from '../core/services/workspace-state.service';
import { RequestApiService } from '../core/services/request.service';
import { ResponseViewer } from '../response-viewer/response-viewer';
import { BinApiService, type HttpBinnedRequestSummary, type RestoreBinnedRequestInput } from '../core/services/bin.service';
import { CollectionApiService, type Collection } from '../core/services/collection.service';

@Component({
  selector: 'app-request-panel',
  imports: [ResponseViewer],
  templateUrl: './request-panel.html',
  styleUrl: './request-panel.scss',
  host: {
    class: 'sidebar sidebar-right',
    'aria-label': 'Request and response panel',
  },
})
export class RequestPanel {
  protected readonly state = inject(WorkspaceStateService);
  private readonly requestApi = inject(RequestApiService);
  private readonly binApi = inject(BinApiService);
  protected readonly collectionApi = inject(CollectionApiService);

  protected readonly responses = this.requestApi.responses;
  protected readonly restoreCollectionModalOpen = signal(false);

  constructor() {
    effect(() => {
      if (this.state.multiSelectionActive() || this.state.showingBin()) {
        this.requestApi.responses.set([]);
        this.state.selectedResponse.set(null);
        return;
      }

      const req = this.state.selectedRequest();
      if (req) {
        this.state.loadResponses(req.id);
      } else {
        this.requestApi.responses.set([]);
        this.state.selectedResponse.set(null);
      }
    });
  }

  openRestorePicker(): void {
    const req = this.state.selectedBinnedRequest();
    if (!req) return;

    const collections = this.collectionApi.collections();
    const originalExists = collections.some(c => c.id === req.original_collection_id);

    if (originalExists) {
      void this.doRestore([{ bin_id: req.id, target_collection_id: req.original_collection_id }]);
      return;
    }

    this.restoreCollectionModalOpen.set(true);
  }

  closeRestoreCollectionModal(): void {
    this.restoreCollectionModalOpen.set(false);
  }

  async restoreToCollection(collection: Collection): Promise<void> {
    const req = this.state.selectedBinnedRequest();
    if (!req) return;

    await this.doRestore([{ bin_id: req.id, target_collection_id: collection.id }]);
    this.closeRestoreCollectionModal();
  }

  private async doRestore(inputs: RestoreBinnedRequestInput[]): Promise<void> {
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
    const req = this.state.selectedBinnedRequest();
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
