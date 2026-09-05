import { Injectable, inject, signal } from '@angular/core';
import { RequestApiService, type HttpRequest, type HttpResponse } from './request.service';
import { FavouriteApiService, type FavouriteCollection } from './favourite.service';
import type { Collection } from './collection.service';
import type { Project } from './project.service';
import type { Environment } from './environment.service';

export type RightPanelMode = 'response' | 'edit';

/**
 * Thin shared state for things that genuinely cross component boundaries:
 * the current selection (project, environment, group, request, response),
 * the right panel mode, a global busy flag, and request execution which can
 * be triggered from both the request list and the request panel.
 *
 * Everything else (UI-local state, popups, loading flows) lives in the
 * component that owns it.
 */
@Injectable({ providedIn: 'root' })
export class WorkspaceStateService {
  private readonly requestApi = inject(RequestApiService);
  private readonly favouriteApi = inject(FavouriteApiService);

  readonly loading = signal(false);
  readonly selectedProject = signal<Project | null>(null);
  readonly selectedEnvironment = signal<Environment | null>(null);
  readonly selectedCollection = signal<Collection | null>(null);
  readonly selectedFavouriteCollection = signal<FavouriteCollection | null>(null);
  readonly selectedTag = signal<string | null>(null);
  readonly selectedRequest = signal<HttpRequest | null>(null);
  readonly selectedResponse = signal<HttpResponse | null>(null);
  readonly rightPanelMode = signal<RightPanelMode>('response');

  readonly requestSendStartTime = signal<number | null>(null);
  readonly requestElapsedMs = signal(0);
  readonly lastResponseDurationMs = signal<number | null>(null);

  private elapsedIntervalId: number | null = null;

  async loadResponses(requestId: number): Promise<void> {
    try {
      await this.requestApi.loadResponsesForRequest(requestId);
      const all = this.requestApi.responses();
      this.selectedResponse.set(all.length > 0 ? all[0] : null);
    } catch (err) {
      console.error(err);
    }
  }

  async sendRequest(req: HttpRequest, event: MouseEvent): Promise<void> {
    event.stopPropagation();
    this.startRequestTimer();
    try {
      const environmentId = this.selectedEnvironment()?.id ?? 0;
      const resp = await this.requestApi.execute(req.id, environmentId);
      if (this.selectedCollection()) {
        await this.requestApi.loadForCollection(this.selectedCollection()!.id);
      }
      if (this.selectedFavouriteCollection()) {
        await this.favouriteApi.loadRequestsForCollection(this.selectedFavouriteCollection()!.id);
      }
      if (this.selectedRequest()?.id === req.id) {
        await this.loadResponses(req.id);
        this.selectedResponse.set(resp);
        this.rightPanelMode.set('response');
      }
    } catch (err) {
      console.error(err);
    } finally {
      this.stopRequestTimer();
    }
  }

  private startRequestTimer(): void {
    const start = Date.now();
    this.requestSendStartTime.set(start);
    this.requestElapsedMs.set(0);
    this.elapsedIntervalId = window.setInterval(() => {
      this.requestElapsedMs.set(Date.now() - start);
    }, 50);
  }

  private stopRequestTimer(): void {
    if (this.elapsedIntervalId !== null) {
      clearInterval(this.elapsedIntervalId);
      this.elapsedIntervalId = null;
    }
    const start = this.requestSendStartTime();
    if (start !== null) {
      this.lastResponseDurationMs.set(Date.now() - start);
    }
    this.requestSendStartTime.set(null);
  }
}
