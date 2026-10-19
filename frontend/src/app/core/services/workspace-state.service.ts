import { computed, effect, Injectable, inject, signal } from '@angular/core';
import { RequestApiService, type HttpRequest, type HttpResponse } from './request.service';
import { ApiRequestsService } from './api-requests.service';
import { ResponseApiService } from './response.service';
import { FavouriteApiService, type FavouriteCollection } from './favourite.service';
import { BinApiService, type HttpBinnedRequestSummary } from './bin.service';
import { EnvironmentVariableApiService } from './environment-variable.service';
import type { Collection } from './collection.service';
import type { Project } from './project.service';
import type { Environment } from './environment.service';

/**
 * Thin shared state for things that genuinely cross component boundaries:
 * the current selection (project, environment, group, request, response),
 * a global busy flag, and request execution which can be triggered from both
 * the request list and the request panel.
 *
 * Everything else (UI-local state, popups, loading flows) lives in the
 * component that owns it.
 */
@Injectable({ providedIn: 'root' })
export class WorkspaceStateService {
  private readonly requestApi = inject(RequestApiService);
  private readonly apiRequests = inject(ApiRequestsService);
  private readonly responseApi = inject(ResponseApiService);
  private readonly favouriteApi = inject(FavouriteApiService);
  private readonly binApi = inject(BinApiService);
  private readonly envVarApi = inject(EnvironmentVariableApiService);

  readonly loading = signal(false);
  readonly selectedProject = signal<Project | null>(null);
  readonly selectedEnvironment = signal<Environment | null>(null);
  readonly selectedCollection = signal<Collection | null>(null);
  readonly selectedFavouriteCollection = signal<FavouriteCollection | null>(null);
  readonly selectedTag = signal<string | null>(null);
  readonly showingAllRequests = signal(false);
  readonly showingFamousRequests = signal(false);
  readonly selectedIds = signal<Set<number>>(new Set());
  readonly selectedRequest = signal<HttpRequest | null>(null);
  readonly selectedResponse = signal<HttpResponse | null>(null);
  readonly selectedBinnedRequest = signal<HttpBinnedRequestSummary | null>(null);
  readonly showingBin = signal(false);
  readonly multiSelectionActive = signal(false);
  readonly zenModeOpen = signal(false);

  readonly environmentVariables = computed(() =>
    this.envVarApi.variables().map((v) => ({ key: v.key, value: v.value })),
  );

  constructor() {
    effect(() => {
      const env = this.selectedEnvironment();
      if (env) {
        void this.envVarApi.loadForEnvironment(env.id);
      } else {
        this.envVarApi.variables.set([]);
      }
    });
  }

  async loadResponses(requestId: number): Promise<void> {
    try {
      await this.requestApi.loadResponsesForRequest(requestId);
      const all = this.requestApi.responses();
      this.selectedResponse.set(all.length > 0 ? all[0] : null);
    } catch (err) {
      console.error(err);
    }
  }

  enterBinView(): void {
    this.selectedCollection.set(null);
    this.selectedFavouriteCollection.set(null);
    this.selectedTag.set(null);
    this.showingAllRequests.set(false);
    this.showingFamousRequests.set(false);
    this.showingBin.set(true);
    this.selectedRequest.set(null);
    this.selectedResponse.set(null);
    this.selectedBinnedRequest.set(null);
  }

  exitBinView(): void {
    this.showingBin.set(false);
    this.selectedBinnedRequest.set(null);
  }

  async loadBinnedRequests(projectId: number): Promise<void> {
    try {
      await this.binApi.loadForProject(projectId);
    } catch (err) {
      console.error(err);
    }
  }

  async sendRequest(req: HttpRequest, event: Event, trackUsage = true): Promise<void> {
    event.stopPropagation();
    try {
      const environmentId = this.selectedEnvironment()?.id ?? 0;
      const resp = await this.responseApi.sendRequest(req, environmentId, {
        onPending: pending => {
          if (this.selectedRequest()?.id === req.id) {
            this.selectedResponse.set(pending);
          }
        },
        onUpdate: updated => {
          if (this.selectedResponse()?.id === updated.id) {
            this.selectedResponse.set(updated);
          }
        },
      }, trackUsage);
      this.requestApi.patchRequest({
        id: req.id,
        status_code: resp.status_code,
        response_id: resp.id,
      });
      this.apiRequests.patch(req.id, {
        status_code: resp.status_code,
        response_id: resp.id,
      });
      this.favouriteApi.requests.update(list =>
        list.map(r =>
          r.id === req.id
            ? { ...r, status_code: resp.status_code, response_id: resp.id }
            : r,
        ),
      );

      if (this.selectedRequest()?.id === req.id) {
        this.selectedResponse.set(resp);
      }
    } catch (err) {
      console.error(err);
    }
  }

  /**
   * Sends multiple requests in one batch. HTTP calls run concurrently and all
   * database writes are committed together by the backend.
   */
  async sendRequests(reqs: HttpRequest[], event: Event, trackUsage = true): Promise<void> {
    event.stopPropagation();
    if (reqs.length === 0) return;

    try {
      const environmentId = this.selectedEnvironment()?.id ?? 0;
      const responses = await this.responseApi.sendRequests(reqs, environmentId, trackUsage);

      for (const resp of responses) {
        this.requestApi.patchRequest({
          id: resp.request_id,
          status_code: resp.status_code,
          response_id: resp.id,
        });
        this.apiRequests.patch(resp.request_id, {
          status_code: resp.status_code,
          response_id: resp.id,
        });
        this.favouriteApi.requests.update(list =>
          list.map(r =>
            r.id === resp.request_id
              ? { ...r, status_code: resp.status_code, response_id: resp.id }
              : r,
          ),
        );

        if (this.selectedRequest()?.id === resp.request_id) {
          this.selectedResponse.set(resp);
        }
      }
    } catch (err) {
      console.error(err);
    }
  }

  openZenMode(): void {
    this.zenModeOpen.set(true);
  }

  closeZenMode(): void {
    this.zenModeOpen.set(false);
  }
}
