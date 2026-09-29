import { Injectable, inject, signal } from '@angular/core';
import { RequestApiService, type HttpRequest, type HttpResponse } from './request.service';
import { ApiRequestsService } from './api-requests.service';
import { ResponseApiService } from './response.service';
import { FavouriteApiService, type FavouriteCollection } from './favourite.service';
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

  readonly loading = signal(false);
  readonly selectedProject = signal<Project | null>(null);
  readonly selectedEnvironment = signal<Environment | null>(null);
  readonly selectedCollection = signal<Collection | null>(null);
  readonly selectedFavouriteCollection = signal<FavouriteCollection | null>(null);
  readonly selectedTag = signal<string | null>(null);
  readonly showingAllRequests = signal(false);
  readonly selectedRequest = signal<HttpRequest | null>(null);
  readonly selectedResponse = signal<HttpResponse | null>(null);
  readonly multiSelectionActive = signal(false);
  readonly zenModeOpen = signal(false);

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
      });
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

  openZenMode(): void {
    this.zenModeOpen.set(true);
  }

  closeZenMode(): void {
    this.zenModeOpen.set(false);
  }
}
