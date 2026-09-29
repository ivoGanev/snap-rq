import { Component, computed, effect, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { WorkspaceStateService } from '../core/services/workspace-state.service';
import { RequestApiService, type HttpRequest, type HttpRequestSummary } from '../core/services/request.service';
import { FavouriteApiService, type FavouriteCollection } from '../core/services/favourite.service';
import { SelectionStateService } from '../core/services/selection-state.service';
import { TagApiService } from '../core/services/tag.service';
import { BinApiService, type HttpBinnedRequestSummary, type RestoreBinnedRequestInput } from '../core/services/bin.service';
import { CollectionApiService, type Collection } from '../core/services/collection.service';

@Component({
  selector: 'app-requests-main-list',
  imports: [FormsModule],
  templateUrl: './requests-main-list.html',
  styleUrl: './requests-main-list.scss',
  host: {
    class: 'main-column',
    'aria-label': 'Requests',
    '(window:keydown)': 'onWindowKeydown($event)',
  },
})
export class RequestsMainList {
  protected readonly state = inject(WorkspaceStateService);
  private readonly requestApi = inject(RequestApiService);
  private readonly favouriteApi = inject(FavouriteApiService);
  private readonly selectionState = inject(SelectionStateService);
  private readonly tagApi = inject(TagApiService);
  private readonly binApi = inject(BinApiService);
  protected readonly collectionApi = inject(CollectionApiService);

  protected readonly requestTags = this.tagApi.requestTags;
  protected readonly favouriteCollections = this.favouriteApi.collections;
  protected readonly favouriteMembership = this.favouriteApi.membership;

  readonly requestSearchQuery = signal('');
  readonly tagRequests = signal<HttpRequestSummary[]>([]);
  readonly requestContextMenuOpen = signal(false);
  readonly requestContextMenuX = signal(0);
  readonly requestContextMenuY = signal(0);
  readonly requestContextMenuTarget = signal<HttpRequestSummary | HttpBinnedRequestSummary | null>(null);
  readonly restoreCollectionModalOpen = signal(false);
  readonly restorePickerBinId = signal<number | null>(null);
  readonly newRequestPopupMode = signal<'manual' | 'curl'>('manual');
  readonly newRequestPopupOpen = signal(false);
  readonly newRequestName = signal('My new snappy API');
  readonly newRequestUrl = signal('');
  readonly newRequestMethod = signal('GET');
  readonly newRequestCurl = signal('');
  readonly favouritePopupOpen = signal(false);
  readonly favouritePopupRequest = signal<HttpRequestSummary | null>(null);
  readonly newFavouriteName = signal('');

  private loadVersion = 0;

  readonly activeRequests = computed<HttpRequestSummary[]>(() => {
    if (this.state.showingBin()) {
      return this.binApi.binnedRequests() as HttpRequestSummary[];
    }
    if (this.state.selectedTag()) {
      return this.tagRequests();
    }
    if (this.state.selectedFavouriteCollection()) {
      return this.favouriteApi.requests();
    }
    if (this.state.selectedCollection()) {
      return this.requestApi.requests();
    }
    if (this.state.showingAllRequests()) {
      return this.requestApi.requests();
    }
    return [];
  });

  readonly activeGroupName = computed<string | null>(() => {
    if (this.state.showingBin()) return 'Trash bin';
    if (this.state.showingAllRequests()) return 'All requests';
    const tag = this.state.selectedTag();
    if (tag) return tag;
    const favourite = this.state.selectedFavouriteCollection();
    if (favourite) return favourite.name;
    const collection = this.state.selectedCollection();
    if (collection) return collection.name;
    return null;
  });

  readonly filteredActiveRequests = computed<HttpRequestSummary[]>(() => {
    const query = this.requestSearchQuery().trim().toLowerCase();
    const requests = this.activeRequests();
    if (!query) return requests;
    return requests.filter(
      (req) =>
        req.name.toLowerCase().includes(query) ||
        req.url.toLowerCase().includes(query) ||
        req.method.toLowerCase().includes(query),
    );
  });

  constructor() {
    effect(() => {
      const collection = this.state.selectedCollection();
      const favourite = this.state.selectedFavouriteCollection();
      const tag = this.state.selectedTag();
      const showAll = this.state.showingAllRequests();
      const showBin = this.state.showingBin();
      const project = this.state.selectedProject();

      const version = ++this.loadVersion;
      this.requestSearchQuery.set('');
      this.state.selectedRequest.set(null);
      this.state.selectedBinnedRequest.set(null);
      this.state.selectedResponse.set(null);
      void this.loadActiveGroup(version, collection?.id ?? null, favourite?.id ?? null, tag, showAll, showBin, project?.id ?? null);
    });
  }

  private async loadActiveGroup(
    version: number,
    collectionId: number | null,
    favouriteId: number | null,
    tag: string | null,
    showAll: boolean,
    showBin: boolean,
    projectId: number | null,
  ): Promise<void> {
    try {
      if (showBin && projectId !== null) {
        await this.binApi.loadForProject(projectId);
        return;
      }

      if (tag) {
        const project = this.state.selectedProject();
        if (!project) return;
        const requests = await this.tagApi.getRequestsForTag(project.id, tag);
        if (version !== this.loadVersion) return;
        this.tagRequests.set(requests);
        await this.tagApi.loadTagsForRequests(requests);
        return;
      }

      if (favouriteId !== null) {
        await this.favouriteApi.loadRequestsForCollection(favouriteId);
        if (version !== this.loadVersion) return;
        const requests = this.favouriteApi.requests();
        await this.tagApi.loadTagsForRequests(requests);
        const rememberedId = this.selectionState.getSelectedRequestForFavourite(favouriteId);
        this.restoreRememberedRequest(requests, rememberedId);
        return;
      }

      if (collectionId !== null) {
        await this.requestApi.loadForCollection(collectionId);
        if (version !== this.loadVersion) return;
        const requests = this.requestApi.requests();
        await this.tagApi.loadTagsForRequests(requests);
        const rememberedId = this.selectionState.getSelectedRequestForCollection(collectionId);
        this.restoreRememberedRequest(requests, rememberedId);
        return;
      }

      if (showAll && projectId !== null) {
        await this.requestApi.loadForProject(projectId);
        if (version !== this.loadVersion) return;
        const requests = this.requestApi.requests();
        await this.tagApi.loadTagsForRequests(requests);
        return;
      }

      this.tagRequests.set([]);
    } catch (err) {
      console.error(err);
    }
  }

  private restoreRememberedRequest(requests: HttpRequestSummary[], rememberedId: number | null): void {
    if (rememberedId === null) return;
    const remembered = requests.find((r) => r.id === rememberedId);
    if (remembered) {
      this.state.selectedRequest.set(null);
      void this.requestApi.get(remembered.id).then(full => {
        if (this.state.selectedRequest()?.id === remembered.id || this.state.selectedRequest() === null) {
          this.state.selectedRequest.set(full);
        }
      }).catch(err => console.error(err));
    }
  }

  onWindowKeydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      this.onEscapePressed();
      return;
    }

    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'd') {
      event.preventDefault();
      const selected = this.state.selectedRequest();
      if (selected) {
        this.requestContextMenuTarget.set(selected);
        void this.duplicateRequest();
      }
    }
  }

  onEscapePressed(): void {
    if (this.requestContextMenuOpen()) {
      this.closeRequestContextMenu();
      return;
    }
    if (this.favouritePopupOpen()) {
      this.closeFavouritePopup();
      return;
    }
    if (this.newRequestPopupOpen()) {
      this.closeNewRequestPopup();
      return;
    }
  }

  async sendRequest(req: HttpRequestSummary | HttpBinnedRequestSummary, event: MouseEvent): Promise<void> {
    event.stopPropagation();
    if (this.state.showingBin()) return;
    try {
      const full = await this.requestApi.get(req.id);
      await this.state.sendRequest(full, event);
    } catch (err) {
      console.error(err);
    }
  }

  selectRequest(req: HttpRequestSummary | HttpBinnedRequestSummary): void {
    if (this.state.showingBin()) {
      this.state.selectedBinnedRequest.set(req as HttpBinnedRequestSummary);
      this.state.selectedRequest.set(null);
      this.state.selectedResponse.set(null);
      return;
    }

    this.state.selectedRequest.set(null);
    this.state.selectedBinnedRequest.set(null);
    this.state.selectedResponse.set(null);
    void this.requestApi.get(req.id).then(full => {
      if (this.state.selectedRequest()?.id === req.id || this.state.selectedRequest() === null) {
        this.state.selectedRequest.set(full);
      }
    }).catch(err => console.error(err));

    const collection = this.state.selectedCollection();
    if (collection) {
      this.selectionState.setSelectedRequestForCollection(collection.id, req.id);
      return;
    }

    const favourite = this.state.selectedFavouriteCollection();
    if (favourite) {
      this.selectionState.setSelectedRequestForFavourite(favourite.id, req.id);
    }
  }

  openZenMode(req: HttpRequestSummary | HttpBinnedRequestSummary, event: MouseEvent): void {
    event.stopPropagation();
    if (this.state.showingBin()) return;
    this.selectRequest(req as HttpRequestSummary);
    this.state.zenModeOpen.set(true);
  }

  openRequestContextMenu(req: HttpRequestSummary | HttpBinnedRequestSummary, event: MouseEvent): void {
    event.preventDefault();
    event.stopPropagation();
    this.requestContextMenuTarget.set(req);
    this.requestContextMenuX.set(event.clientX);
    this.requestContextMenuY.set(event.clientY);
    this.requestContextMenuOpen.set(true);
  }

  closeRequestContextMenu(): void {
    this.requestContextMenuOpen.set(false);
    this.requestContextMenuTarget.set(null);
  }

  async copyRequestCurl(): Promise<void> {
    const req = this.requestContextMenuTarget();
    if (!req || this.state.showingBin()) return;

    try {
      const full = await this.requestApi.get(req.id);
      const curl = await this.requestApi.requestToCurl(full);
      await navigator.clipboard.writeText(curl);
    } catch (err) {
      console.error(err);
    } finally {
      this.closeRequestContextMenu();
    }
  }

  async duplicateRequest(): Promise<void> {
    const req = this.requestContextMenuTarget();
    if (!req) return;

    this.state.loading.set(true);
    try {
      const duplicated = await this.requestApi.duplicate(req.id);

      const favourite = this.state.selectedFavouriteCollection();
      if (favourite && (this.favouriteApi.requestsMembership()[req.id] ?? []).includes(favourite.id)) {
        this.favouriteApi.requests.update(list =>
          [...list, duplicated].sort((a, b) => a.name.localeCompare(b.name)),
        );
      }

      this.selectRequest(duplicated);
      this.closeRequestContextMenu();
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }

  async deleteRequest(): Promise<void> {
    const req = this.requestContextMenuTarget();
    if (!req) return;

    this.state.loading.set(true);
    try {
      if (this.state.showingBin()) {
        await this.binApi.deletePermanently(req.id);
        if (this.state.selectedBinnedRequest()?.id === req.id) {
          this.state.selectedBinnedRequest.set(null);
        }
        this.closeRequestContextMenu();
        return;
      }

      const collection = this.state.selectedCollection();
      const favourite = this.state.selectedFavouriteCollection();
      const tag = this.state.selectedTag();

      if (favourite) {
        await this.favouriteApi.removeRequest(favourite.id, req.id);
        this.selectionState.setSelectedRequestForFavourite(favourite.id, null);
        this.favouriteApi.requests.update(list => list.filter(r => r.id !== req.id));
      } else {
        await this.requestApi.delete(req.id);
        this.selectionState.deleteRequest(req.id);
        if (tag) {
          this.tagRequests.update(list => list.filter(r => r.id !== req.id));
        }
      }

      if (this.state.selectedRequest()?.id === req.id) {
        this.state.selectedRequest.set(null);
        this.state.selectedResponse.set(null);
      }

      this.closeRequestContextMenu();
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }

  openRestorePicker(): void {
    const req = this.requestContextMenuTarget();
    if (!req || !this.state.showingBin()) return;

    const binned = req as HttpBinnedRequestSummary;
    const collections = this.collectionApi.collections();
    const originalExists = collections.some(c => c.id === binned.original_collection_id);

    if (originalExists) {
      void this.doRestore([{ bin_id: binned.id, target_collection_id: binned.original_collection_id } as RestoreBinnedRequestInput]);
      this.closeRequestContextMenu();
      return;
    }

    this.restorePickerBinId.set(binned.id);
    this.restoreCollectionModalOpen.set(true);
    this.closeRequestContextMenu();
  }

  closeRestoreCollectionModal(): void {
    this.restoreCollectionModalOpen.set(false);
    this.restorePickerBinId.set(null);
  }

  async restoreToCollection(collection: Collection): Promise<void> {
    const binId = this.restorePickerBinId();
    if (binId === null) return;

    await this.doRestore([{ bin_id: binId, target_collection_id: collection.id }]);
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

  openNewRequestPopup(): void {
    this.newRequestName.set('My new snappy API');
    this.newRequestUrl.set('');
    this.newRequestMethod.set('GET');
    this.newRequestCurl.set('');
    this.newRequestPopupMode.set('manual');
    this.newRequestPopupOpen.set(true);
  }

  closeNewRequestPopup(): void {
    this.newRequestPopupOpen.set(false);
  }

  setNewRequestPopupMode(mode: 'manual' | 'curl'): void {
    this.newRequestPopupMode.set(mode);
  }

  async addRequest(): Promise<void> {
    const collection = this.state.selectedCollection();
    if (!collection) return;

    const mode = this.newRequestPopupMode();
    if (mode === 'curl') {
      await this.addRequestFromCurl(collection.id);
      return;
    }

    const name = this.newRequestName().trim();
    if (!name) return;

    this.state.loading.set(true);
    try {
      await this.requestApi.create({
        collection_id: collection.id,
        project_id: collection.project_id,
        name,
        url: this.newRequestUrl().trim(),
        method: this.newRequestMethod(),
        body: '',
        request_headers: '',
        status_code: 0,
        response_id: 0,
      });
      this.closeNewRequestPopup();
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }

  private async addRequestFromCurl(collectionId: number): Promise<void> {
    const curl = this.newRequestCurl().trim();
    if (!curl) return;

    this.state.loading.set(true);
    try {
      const projectId = this.state.selectedProject()?.id ?? 0;
      const req = await this.requestApi.curlToRequest(collectionId, curl);
      await this.requestApi.create({
        collection_id: collectionId,
        project_id: projectId,
        name: req.name,
        url: req.url,
        method: req.method,
        body: req.body,
        request_headers: req.request_headers,
        status_code: 0,
        response_id: 0,
      });
      this.closeNewRequestPopup();
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }

  openFavouritePopup(req: HttpRequestSummary | HttpBinnedRequestSummary, event: MouseEvent): void {
    event.stopPropagation();
    if (this.state.showingBin()) return;
    const request = req as HttpRequestSummary;
    this.favouritePopupRequest.set(request);
    this.favouritePopupOpen.set(true);
    this.newFavouriteName.set('');
    void this.favouriteApi.loadMembershipForRequest(request.id);
  }

  closeFavouritePopup(): void {
    this.favouritePopupOpen.set(false);
    this.favouritePopupRequest.set(null);
    this.favouriteApi.clearMembership();
  }

  async toggleFavouriteMembership(collection: FavouriteCollection): Promise<void> {
    const req = this.favouritePopupRequest();
    if (!req) return;

    const isMember = this.favouriteMembership().has(collection.id);
    try {
      if (isMember) {
        await this.favouriteApi.removeRequest(collection.id, req.id);
      } else {
        await this.favouriteApi.addRequest(collection.id, req.id);
      }
      if (this.state.selectedFavouriteCollection()?.id === collection.id) {
        this.favouriteApi.requests.update(list =>
          isMember ? list.filter(r => r.id !== req.id) : [...list, req].sort((a, b) => a.name.localeCompare(b.name)),
        );
      }
    } catch (err) {
      console.error(err);
    }
  }

  async addFavouriteCollection(): Promise<void> {
    const name = this.newFavouriteName().trim();
    const project = this.state.selectedProject();
    if (!name || !project) return;

    try {
      await this.favouriteApi.createCollection({ project_id: project.id, name });
      this.newFavouriteName.set('');
    } catch (err) {
      console.error(err);
    }
  }

  async deleteFavouriteCollection(
    collection: FavouriteCollection,
    event: MouseEvent,
  ): Promise<void> {
    event.stopPropagation();
    try {
      await this.favouriteApi.deleteCollection(collection.id);
      if (this.state.selectedFavouriteCollection()?.id === collection.id) {
        this.state.selectedFavouriteCollection.set(null);
        this.favouriteApi.requests.set([]);
      }
    } catch (err) {
      console.error(err);
    }
  }
}
