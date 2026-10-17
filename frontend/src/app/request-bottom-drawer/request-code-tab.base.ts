import { computed, effect, inject, signal } from '@angular/core';
import { EnvironmentVariableApiService } from '../core/services/environment-variable.service';
import { type HttpRequest } from '../core/services/request.service';
import { RequestDrawerTabBase } from './request-drawer-tab.base';

/**
 * Shared behaviour for drawer tabs backed by the code editor.
 *
 * Adds draft/baseline tracking, debounced autosave for a single selection,
 * bulk apply, a mismatch clear-all flow, and environment variable loading on
 * top of {@link RequestDrawerTabBase}.
 */
export abstract class RequestCodeTabBase<T> extends RequestDrawerTabBase<T> {
  private readonly envVarApi = inject(EnvironmentVariableApiService);

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

  readonly environmentVariables = computed(() =>
    this.envVarApi.variables().map((v) => ({ key: v.key, value: v.value })),
  );

  private saveTimer: number | null = null;

  constructor() {
    super();

    effect(() => {
      const env = this.state.selectedEnvironment();
      if (env) {
        void this.envVarApi.loadForEnvironment(env.id);
      } else {
        this.envVarApi.variables.set([]);
      }
    });
  }

  /** The value used when clearing all selected requests. */
  protected abstract emptyValue(): T;

  /** Replaces the draft and schedules autosave for a single selection. */
  protected updateDraft(value: T): void {
    this.draftValue.set(value);
    if (this.editCount() === 1) {
      this.scheduleAutosave();
    }
  }

  protected override beforeReload(): void {
    this.clearSaveTimer();
    const draft = this.draftValue();
    if (this.requests().length === 1 && draft !== null && this.dirty()) {
      void this.flush(draft);
    }
  }

  protected override onLoaded(_requests: HttpRequest[], value: T | null): void {
    this.baselineValue.set(value);
    this.draftValue.set(value);
  }

  /** Flushes pending autosave and applies the draft (multi-selection). */
  protected async applyDraft(): Promise<void> {
    this.clearSaveTimer();
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

  private scheduleAutosave(): void {
    this.clearSaveTimer();
    this.saveTimer = window.setTimeout(() => {
      this.saveTimer = null;
      void this.commitDraft();
    }, 400);
  }

  private clearSaveTimer(): void {
    if (this.saveTimer !== null) {
      window.clearTimeout(this.saveTimer);
      this.saveTimer = null;
    }
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
