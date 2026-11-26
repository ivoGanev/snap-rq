import { computed, signal } from '@angular/core';
import { type HttpRequest } from '../core/services/request.service';
import { RequestDrawerTabBase } from './request-drawer-tab.base';

/**
 * Shared behaviour for drawer tabs backed by the code editor.
 *
 * Adds draft/baseline tracking, saving as you type for a single selection,
 * bulk apply, a mismatch clear-all flow, and environment variable loading on
 * top of {@link RequestDrawerTabBase}.
 */
export abstract class RequestCodeTabBase<T> extends RequestDrawerTabBase<T> {
  readonly draftValue = signal<T | null>(null);
  readonly baselineValue = signal<T | null>(null);
  readonly dirty = computed(() => {
    const draft = this.draftValue();
    const baseline = this.baselineValue();
    if (draft === null || baseline === null) return false;
    return !this.valueEquals(draft, baseline);
  });

  readonly wipeConfirmOpen = signal(false);
  readonly wipeCount = signal(0);

  readonly environmentVariables = this.state.environmentVariables;

  /** The value used when clearing all selected requests. */
  protected abstract emptyValue(): T;

  /** Replaces the draft; a single selection saves it straight away. */
  protected updateDraft(value: T): void {
    this.draftValue.set(value);
    if (this.editCount() === 1) {
      void this.commitDraft();
    }
  }

  protected override onLoaded(_requests: HttpRequest[], value: T | null): void {
    this.baselineValue.set(value);
    this.draftValue.set(value);
  }

  /** Applies the draft to every selected request (multi-selection). */
  protected async applyDraft(): Promise<void> {
    await this.commitDraft();
  }

  openWipeConfirm(): void {
    const ids = this.state.selectedIds();
    if (ids.size === 0) return;
    this.wipeCount.set(ids.size);
    this.wipeConfirmOpen.set(true);
  }

  closeWipeConfirm(): void {
    this.wipeConfirmOpen.set(false);
  }

  async confirmClearAll(): Promise<void> {
    if (this.requests().length === 0) {
      this.closeWipeConfirm();
      return;
    }

    const value = this.emptyValue();
    await this.save(value);
    this.baselineValue.set(value);
    this.draftValue.set(value);
    this.closeWipeConfirm();
  }

  private async commitDraft(): Promise<void> {
    const draft = this.draftValue();
    if (draft === null) return;

    const saved = await this.save(draft);
    if (saved.length > 0) {
      this.baselineValue.set(draft);
    }
  }
}
