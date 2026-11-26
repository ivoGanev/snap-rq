import { computed, effect, Injectable, inject, signal } from '@angular/core';
import { RequestApiService, type HttpRequest, type HttpResponse } from './request.service';
import { FavouriteApiService, type FavouriteCollection } from './favourite.service';
import { BinApiService, type HttpBinnedRequestSummary } from './bin.service';
import { EnvironmentVariableApiService } from './environment-variable.service';
import { EnvironmentApiService } from './environment.service';
import { ChangeBus } from './change-bus.service';
import { EnvironmentContext } from '../models/environment-context';
import { AllRequestsView, isSmartView, type SmartView, type WorkspaceView } from '../workspace/workspace-view';
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
  private readonly openView = signal<WorkspaceView | null>(null);
  /** The view the workspace is showing, or null when none is open. Change it with `selectView`. */
  readonly view = this.openView.asReadonly();

  // Read-only shortcuts derived from `view`, kept so existing readers don't
  // have to change at once (step A3 of docs/plans/tags-to-scripts.md).
  readonly selectedCollection = computed<Collection | null>(() => {
    const view = this.openView();
    return view?.kind === 'collection' ? view.collection : null;
  });
  readonly selectedFavouriteCollection = computed<FavouriteCollection | null>(() => {
    const view = this.openView();
    return view?.kind === 'favourite' ? view.favourite : null;
  });
  readonly selectedScript = computed<string | null>(() => {
    const view = this.openView();
    return view?.kind === 'script' ? view.name : null;
  });
  readonly showingAllRequests = computed(() => this.openView()?.kind === 'all');
  /** The open smart view (Recent, Failing, Not used lately), if one is open. */
  readonly smartView = computed<SmartView | null>(() => {
    const view = this.openView();
    return isSmartView(view) ? view : null;
  });
  readonly showingBin = computed(() => this.openView()?.kind === 'bin');

  readonly selectedIds = signal<Set<number>>(new Set());
  readonly selectedRequest = signal<HttpRequest | null>(null);
  readonly selectedResponse = signal<HttpResponse | null>(null);
  readonly responsesLoading = signal(false);
  readonly selectedBinnedRequest = signal<HttpBinnedRequestSummary | null>(null);
  readonly sidebarExpanded = signal(false);
  /** The request list's search, typed in the top bar. */
  readonly requestSearchQuery = signal('');
  /** Set by the top bar's add buttons; the request list carries it out. */
  readonly addRequestIntent = signal<'blank' | 'curl' | null>(null);
  readonly sidebarListOpen = signal(false);
  readonly multiSelectionActive = signal(false);
  readonly zenModeOpen = signal(false);

  readonly environmentVariables = computed(() =>
    this.envVarApi.variables().map((v) => ({
      key: v.key,
      value: v.value,
      secret: v.secret,
      hasValue: v.has_value,
    })),
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

  /**
   * Switch to `view`. Entering the trash bin clears the open request (its
   * rows are not live requests); leaving it clears the selected binned row.
   */
  selectView(view: WorkspaceView | null): void {
    const leavingBin = this.openView()?.kind === 'bin' && view?.kind !== 'bin';
    this.openView.set(view);

    if (view?.kind === 'bin' || view?.main === 'script') {
      // Neither shows live requests: drop the open one.
      this.selectedRequest.set(null);
      this.selectedResponse.set(null);
      this.selectedBinnedRequest.set(null);
      if (view.main === 'script') {
        // The request list is not shown, so nothing in it can stay selected.
        this.selectedIds.set(new Set());
        this.multiSelectionActive.set(false);
      }
    } else if (leavingBin) {
      this.selectedBinnedRequest.set(null);
    }
  }

  /**
   * After switching project: views that exist in every project (all, recent,
   * trash) stay open; a view of one of the old project's groups falls back
   * to all requests.
   */
  selectViewForNewProject(): void {
    const current = this.openView();
    this.selectView(current?.survivesProjectSwitch ? current : new AllRequestsView());
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
      await this.requestApi.send(req, this.selectedEnvironment()?.id ?? null);
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
