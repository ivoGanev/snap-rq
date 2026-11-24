import { Component, computed, inject, input, signal } from '@angular/core';
import { InspectorHeader } from '../../inspector-header/inspector-header';
import type { InspectorContentOf } from '../../inspector';
import { WorkspaceStateService } from '../../../core/services/workspace-state.service';
import { BinApiService } from '../../../core/services/bin.service';
import { ConfirmService } from '../../../core/services/confirm.service';
import { RestoreToCollectionDialog } from '../../../dialogs/restore-to-collection-dialog/restore-to-collection-dialog';

const LISTED = 8;

/** Inspector pane for several requests selected in the trash bin. */
@Component({
  selector: 'app-binned-requests-pane',
  imports: [InspectorHeader, RestoreToCollectionDialog],
  templateUrl: './binned-requests-pane.html',
  styleUrl: '../binned-request-pane/binned-request-pane.scss',
})
export class BinnedRequestsPane {
  private readonly state = inject(WorkspaceStateService);
  private readonly binApi = inject(BinApiService);
  private readonly confirm = inject(ConfirmService);

  readonly content = input.required<InspectorContentOf<'binned-requests'>>();

  protected readonly requests = computed(() => {
    const ids = new Set(this.content().binIds);
    return this.binApi.binnedRequests().filter((r) => ids.has(r.id));
  });
  protected readonly listed = computed(() => this.requests().slice(0, LISTED));
  protected readonly more = computed(() => Math.max(0, this.requests().length - LISTED));
  protected readonly restorePickerOpen = signal(false);

  private done(): void {
    this.state.selectedBinnedRequest.set(null);
    this.state.selectedIds.set(new Set());
    this.state.multiSelectionActive.set(false);
  }

  async restoreAll(): Promise<void> {
    const ids = this.requests().map((r) => r.id);
    if (ids.length === 0) return;
    this.state.loading.set(true);
    try {
      if (await this.binApi.restoreToOriginal(ids)) {
        this.done();
      } else {
        this.restorePickerOpen.set(true);
      }
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }

  async deleteAll(): Promise<void> {
    const ids = this.requests().map((r) => r.id);
    if (ids.length === 0) return;
    const confirmed = await this.confirm.ask({
      title: 'Delete permanently?',
      message: `These ${ids.length} requests will be permanently deleted. This cannot be undone.`,
      confirmLabel: 'Delete permanently',
      danger: true,
    });
    if (!confirmed) return;

    this.state.loading.set(true);
    try {
      await this.binApi.deleteManyPermanently(ids);
      this.done();
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }

  protected onPickerClosed(): void {
    this.restorePickerOpen.set(false);
    if (this.requests().length === 0) this.done();
  }
}
