import { Component, computed, inject, input, signal } from '@angular/core';
import { InspectorHeader } from '../../inspector-header/inspector-header';
import type { InspectorContentOf } from '../../inspector';
import { WorkspaceStateService } from '../../../core/services/workspace-state.service';
import { BinApiService } from '../../../core/services/bin.service';
import { ConfirmService } from '../../../core/services/confirm.service';
import { parseSavedTime, relativeTime } from '../../../core/time';
import { RestoreToCollectionDialog } from '../../../dialogs/restore-to-collection-dialog/restore-to-collection-dialog';

/**
 * Inspector pane for a request in the trash bin: shows where it came from and
 * lets the user restore it (to its original collection, or a picked one when
 * that collection is gone) or delete it permanently.
 */
@Component({
  selector: 'app-binned-request-pane',
  imports: [InspectorHeader, RestoreToCollectionDialog],
  templateUrl: './binned-request-pane.html',
  styleUrl: './binned-request-pane.scss',
})
export class BinnedRequestPane {
  private readonly state = inject(WorkspaceStateService);
  private readonly binApi = inject(BinApiService);
  private readonly confirm = inject(ConfirmService);

  readonly content = input.required<InspectorContentOf<'binned-request'>>();

  protected readonly request = computed(
    () => this.binApi.binnedRequests().find((r) => r.id === this.content().binId) ?? null,
  );
  protected readonly originalName = computed(() => {
    const req = this.request();
    return req ? this.binApi.originalCollectionName(req) : null;
  });
  protected readonly deletedAgo = computed(() => {
    const req = this.request();
    return req ? relativeTime(parseSavedTime(req.deleted_at), Date.now()) : '';
  });
  /** Open when the request's original collection is gone and the user must pick one. */
  protected readonly restorePickerOpen = signal(false);

  async restore(): Promise<void> {
    const req = this.request();
    if (!req) return;
    this.state.loading.set(true);
    try {
      if (await this.binApi.restoreToOriginal([req.id])) {
        this.state.selectedBinnedRequest.set(null);
      } else {
        this.restorePickerOpen.set(true);
      }
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }

  async deletePermanently(): Promise<void> {
    const req = this.request();
    if (!req) return;
    const confirmed = await this.confirm.ask({
      title: 'Delete permanently?',
      message: `"${req.name}" will be permanently deleted. This cannot be undone.`,
      confirmLabel: 'Delete permanently',
      danger: true,
    });
    if (!confirmed) return;

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
