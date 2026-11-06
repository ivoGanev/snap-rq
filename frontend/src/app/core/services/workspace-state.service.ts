import { computed, effect, Injectable, inject, signal } from '@angular/core';
import { RequestApiService, type HttpRequest, type HttpResponse } from './request.service';
import { FavouriteApiService, type FavouriteCollection } from './favourite.service';
import { BinApiService, type HttpBinnedRequestSummary } from './bin.service';
import { EnvironmentVariableApiService } from './environment-variable.service';
import { EnvironmentApiService } from './environment.service';
import { ChangeBus } from './change-bus.service';
import { EnvironmentContext } from '../models/environment-context';
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
  private readonly favouriteApi = inject(FavouriteApiService);
  private readonly binApi = inject(BinApiService);
  private readonly envVarApi = inject(EnvironmentVariableApiService);
  private readonly environmentApi = inject(EnvironmentApiService);
  private readonly changeBus = inject(ChangeBus);

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
  readonly responsesLoading = signal(false);
  readonly selectedBinnedRequest = signal<HttpBinnedRequestSummary | null>(null);
  readonly showingBin = signal(false);
  readonly sidebarExpanded = signal(false);
  readonly sidebarListOpen = signal(false);
  readonly multiSelectionActive = signal(false);
  readonly zenModeOpen = signal(false);

  readonly environmentVariables = computed(() =>
    this.envVarApi.variables().map((v) => ({ key: v.key, value: v.value })),
  );

  /**
   * The selected environment as an interpolation entity. Recomputes whenever
   * the environment or its variables change, so display and execution always
   * see the same resolved values.
   */
  readonly environment = computed(() =>
    new EnvironmentContext(
      this.selectedEnvironment()?.id ?? null,
      this.selectedEnvironment()?.name ?? '',
      this.environmentVariables(),
    ),
  );

  constructor() {
    // Variables follow the selected environment.
    effect(() => {
      const env = this.selectedEnvironment();
      if (env) {
        void this.envVarApi.loadForEnvironment(env.id);
      } else {
        this.envVarApi.variables.set([]);
      }
    });

    // If the selected environment disappears (deleted, or its project
    // changed), fall back to the first one available.
    effect(() => {
      const environments = this.environmentApi.environments();
      const selected = this.selectedEnvironment();
      if (selected && environments.some((env) => env.id === selected.id)) {
        return;
      }
      this.selectedEnvironment.set(environments[0] ?? null);
    });

    // When a command touches the open request, reload its full row (the
    // summary refresh cannot update body/headers).
    this.changeBus.changesFor('request').subscribe(change => {
      void this.refreshSelectedRequest(change.ids);
    });

    // When a send finishes for the open request, surface its saved response.
    this.requestApi.finished$.subscribe(resp => {
      if (this.selectedRequest()?.id === resp.request_id) {
        void this.loadResponses(resp.request_id);
      }
    });
  }

  private async refreshSelectedRequest(ids: number[]): Promise<void> {
    const selected = this.selectedRequest();
    if (!selected || !ids.includes(selected.id)) return;
    try {
      this.selectedRequest.set(await this.requestApi.getFull(selected.id));
    } catch {
      // the row is gone (deleted); the list store handles removal
    }
  }

  async loadResponses(requestId: number): Promise<void> {
    this.responsesLoading.set(true);
    try {
      await this.requestApi.loadResponsesForRequest(requestId);
      const all = this.requestApi.responses();
      this.selectedResponse.set(all.length > 0 ? all[0] : null);
    } catch (err) {
      console.error(err);
    } finally {
      this.responsesLoading.set(false);
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

  async sendRequest(req: HttpRequest, event: Event): Promise<void> {
    event.stopPropagation();
    try {
      await this.requestApi.send(req, this.environmentVariables());
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
