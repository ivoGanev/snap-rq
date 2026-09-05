import { Component, computed, effect, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { WorkspaceStateService } from '../core/services/workspace-state.service';
import { RequestApiService, type HttpRequest } from '../core/services/request.service';
import { FavouriteApiService, type FavouriteCollection } from '../core/services/favourite.service';
import { SelectionStateService } from '../core/services/selection-state.service';
import { TagApiService } from '../core/services/tag.service';

@Component({
  selector: 'app-requests-main-list',
  imports: [FormsModule],
  templateUrl: './requests-main-list.html',
  styleUrl: './requests-main-list.scss',
  host: {
    class: 'main-column',
    'aria-label': 'Requests',
    '(window:keydown.escape)': 'onEscapePressed()',
  },
})
export class RequestsMainList {
  protected readonly state = inject(WorkspaceStateService);
  private readonly requestApi = inject(RequestApiService);
  private readonly favouriteApi = inject(FavouriteApiService);
  private readonly selectionState = inject(SelectionStateService);
  private readonly tagApi = inject(TagApiService);

  protected readonly requestTags = this.tagApi.requestTags;
  protected readonly favouriteCollections = this.favouriteApi.collections;
  protected readonly favouriteMembership = this.favouriteApi.membership;

  readonly requestSearchQuery = signal('');
  readonly tagRequests = signal<HttpRequest[]>([]);
  readonly requestContextMenuOpen = signal(false);
  readonly requestContextMenuX = signal(0);
  readonly requestContextMenuY = signal(0);
  readonly requestContextMenuTarget = signal<HttpRequest | null>(null);
  readonly newRequestPopupOpen = signal(false);
  readonly newRequestName = signal('My new snappy API');
  readonly newRequestUrl = signal('');
  readonly newRequestMethod = signal('GET');
  readonly favouritePopupOpen = signal(false);
  readonly favouritePopupRequest = signal<HttpRequest | null>(null);
  readonly newFavouriteName = signal('');

  private loadVersion = 0;

  readonly activeRequests = computed<HttpRequest[]>(() => {
    if (this.state.selectedTag()) {
      return this.tagRequests();
    }
    if (this.state.selectedFavouriteCollection()) {
      return this.favouriteApi.requests();
    }
    if (this.state.selectedCollection()) {
      return this.requestApi.requests();
    }
    return [];
  });

  readonly activeGroupName = computed<string | null>(() => {
    const tag = this.state.selectedTag();
    if (tag) return tag;
    const favourite = this.state.selectedFavouriteCollection();
    if (favourite) return favourite.name;
    const collection = this.state.selectedCollection();
    if (collection) return collection.name;
    return null;
  });

  readonly filteredActiveRequests = computed<HttpRequest[]>(() => {
    const query = this.requestSearchQuery().trim().toLowerCase();
    const requests = this.activeRequests();
    if (!query) return requests;
    return requests.filter(
      req =>
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

      const version = ++this.loadVersion;
      this.requestSearchQuery.set('');
      this.state.selectedRequest.set(null);
      void this.loadActiveGroup(version, collection?.id ?? null, favourite?.id ?? null, tag);
    });
  }

  private async loadActiveGroup(
    version: number,
    collectionId: number | null,
    favouriteId: number | null,
    tag: string | null,
  ): Promise<void> {
    try {
      if (tag) {
        const requests = await this.tagApi.getRequestsForTag(tag);
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

      this.tagRequests.set([]);
    } catch (err) {
      console.error(err);
    }
  }

  private restoreRememberedRequest(requests: HttpRequest[], rememberedId: number | null): void {
    if (rememberedId === null) return;
    const remembered = requests.find(r => r.id === rememberedId);
    if (remembered) {
      this.state.selectedRequest.set(remembered);
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

  selectRequest(req: HttpRequest): void {
    this.state.selectedRequest.set(req);
    this.state.rightPanelMode.set('response');

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

  openRequestContextMenu(req: HttpRequest, event: MouseEvent): void {
    event.preventDefault();
    this.requestContextMenuTarget.set(req);
    this.requestContextMenuX.set(event.clientX);
    this.requestContextMenuY.set(event.clientY);
    this.requestContextMenuOpen.set(true);
  }

  closeRequestContextMenu(): void {
    this.requestContextMenuOpen.set(false);
    this.requestContextMenuTarget.set(null);
  }

  async deleteRequest(): Promise<void> {
    const req = this.requestContextMenuTarget();
    if (!req) return;

    const collection = this.state.selectedCollection();
    const favourite = this.state.selectedFavouriteCollection();

    this.state.loading.set(true);
    try {
      if (collection) {
        await this.requestApi.delete(req.id);
        this.selectionState.deleteRequest(req.id);
      } else if (favourite) {
        await this.favouriteApi.removeRequest(favourite.id, req.id);
        this.selectionState.setSelectedRequestForFavourite(favourite.id, null);
      }

      if (this.state.selectedRequest()?.id === req.id) {
        this.state.selectedRequest.set(null);
        this.state.selectedResponse.set(null);
        this.state.rightPanelMode.set('response');
      }

      if (collection) {
        await this.requestApi.loadForCollection(collection.id);
      }
      if (favourite) {
        await this.favouriteApi.loadRequestsForCollection(favourite.id);
      }

      this.closeRequestContextMenu();
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
    this.newRequestPopupOpen.set(true);
  }

  closeNewRequestPopup(): void {
    this.newRequestPopupOpen.set(false);
  }

  async addRequest(): Promise<void> {
    const collection = this.state.selectedCollection();
    if (!collection) return;

    const name = this.newRequestName().trim();
    if (!name) return;

    this.state.loading.set(true);
    try {
      await this.requestApi.create({
        collection_id: collection.id,
        name,
        url: this.newRequestUrl().trim(),
        method: this.newRequestMethod(),
        body: '',
        request_headers: '',
        status_code: 0,
        response_id: 0,
      });
      await this.requestApi.loadForCollection(collection.id);
      this.closeNewRequestPopup();
    } catch (err) {
      console.error(err);
    } finally {
      this.state.loading.set(false);
    }
  }

  openFavouritePopup(req: HttpRequest, event: MouseEvent): void {
    event.stopPropagation();
    this.favouritePopupRequest.set(req);
    this.favouritePopupOpen.set(true);
    this.newFavouriteName.set('');
    this.favouriteApi.loadMembershipForRequest(req.id);
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
        await this.favouriteApi.loadRequestsForCollection(collection.id);
      }
    } catch (err) {
      console.error(err);
    }
  }

  async addFavouriteCollection(): Promise<void> {
    const name = this.newFavouriteName().trim();
    const profile = this.state.selectedProject();
    if (!name || !profile) return;

    try {
      await this.favouriteApi.createCollection({ profile_id: profile.profile_id, name });
      this.newFavouriteName.set('');
    } catch (err) {
      console.error(err);
    }
  }

  async deleteFavouriteCollection(collection: FavouriteCollection, event: MouseEvent): Promise<void> {
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
