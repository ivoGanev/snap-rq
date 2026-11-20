import { Component, computed, inject, input, output, signal, type OnDestroy, type OnInit } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { FavouriteApiService, type FavouriteCollection } from '../../core/services/favourite.service';
import { WorkspaceStateService } from '../../core/services/workspace-state.service';
import type { HttpRequestSummary } from '../../core/models/http';

/**
 * Add one request or several to favourite groups: a checkbox per group
 * (ticked when every request is in it), plus creating and deleting groups.
 */
@Component({
  selector: 'app-favourite-picker-dialog',
  imports: [FormsModule],
  templateUrl: './favourite-picker-dialog.html',
  styleUrl: './favourite-picker-dialog.scss',
  host: {
    '(document:keydown.escape)': 'closed.emit()',
  },
})
export class FavouritePickerDialog implements OnInit, OnDestroy {
  private readonly favouriteApi = inject(FavouriteApiService);
  private readonly state = inject(WorkspaceStateService);

  /** The requests to add: one from a row, several from a bulk selection. */
  readonly requests = input.required<HttpRequestSummary[]>();
  readonly closed = output<void>();

  protected readonly collections = this.favouriteApi.collections;
  protected readonly newGroupName = signal('');

  protected readonly title = computed(() =>
    this.requests().length === 1 ? 'Favourites' : `Favourites — ${this.requests().length} requests`,
  );

  ngOnInit(): void {
    // Loaded once on open; the toggles below keep it up to date.
    void this.favouriteApi.loadMembershipForRequests(this.requests());
  }

  ngOnDestroy(): void {
    this.favouriteApi.clearMembership();
  }

  /** True when every request is in the group. */
  protected containsAll(collectionId: number): boolean {
    const membership = this.favouriteApi.requestsMembership();
    return this.requests().every(req => (membership[req.id] ?? []).includes(collectionId));
  }

  /** Removes every request from the group if all are in it, otherwise adds them all. */
  protected async toggle(collection: FavouriteCollection): Promise<void> {
    const requests = this.requests();
    const allIn = this.containsAll(collection.id);
    this.state.loading.set(true);
    try {
      for (const req of requests) {
        if (allIn) {
          await this.favouriteApi.removeRequest(collection.id, req.id);
        } else {
          await this.favouriteApi.addRequest(collection.id, req.id);
        }
      }

      const ids = requests.map(r => r.id);
      this.favouriteApi.requestsMembership.update(map => {
        const next = { ...map };
        for (const id of ids) {
          const current = next[id] ?? [];
          next[id] = allIn
            ? current.filter(cid => cid !== collection.id)
            : Array.from(new Set([...current, collection.id]));
        }
        return next;
      });

      // Keep the open favourite view in step.
      if (this.state.selectedFavouriteCollection()?.id === collection.id) {
        if (allIn) {
          this.favouriteApi.requests.update(list => list.filter(r => !ids.includes(r.id)));
        } else {
          this.favouriteApi.requests.update(list => {
            const existingIds = new Set(list.map(r => r.id));
            const added = requests.filter(r => !existingIds.has(r.id));
            return added.length > 0 ? [...list, ...added].sort((a, b) => a.name.localeCompare(b.name)) : list;
          });
        }
      }
    } catch (err) {
      console.error('updating favourites failed', err);
    } finally {
      this.state.loading.set(false);
    }
  }

  protected async addGroup(): Promise<void> {
    const name = this.newGroupName().trim();
    const project = this.state.selectedProject();
    if (!name || !project) return;
    try {
      await this.favouriteApi.createCollection({ project_id: project.id, name });
      this.newGroupName.set('');
    } catch (err) {
      console.error('creating favourite group failed', err);
    }
  }

  protected async deleteGroup(collection: FavouriteCollection, event: MouseEvent): Promise<void> {
    event.stopPropagation();
    try {
      await this.favouriteApi.deleteCollection(collection.id);
      if (this.state.selectedFavouriteCollection()?.id === collection.id) {
        this.state.selectView(null);
        this.favouriteApi.requests.set([]);
      }
    } catch (err) {
      console.error('deleting favourite group failed', err);
    }
  }
}
