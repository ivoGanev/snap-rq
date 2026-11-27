import { Injectable, effect, inject, signal, untracked } from '@angular/core';
import { WorkspaceStateService } from './workspace-state.service';
import { CollectionApiService } from './collection.service';
import { FavouriteApiService } from './favourite.service';
import { ScriptApiService } from './script.service';
import {
  AllRequestsView,
  CollectionView,
  FailingView,
  FavouriteView,
  MostUsedView,
  RecentView,
  ScriptView,
  TrashView,
  UnusedView,
  type WorkspaceView,
} from '../workspace/workspace-view';

export type SmartViewId = 'all' | 'recent' | 'usage' | 'failing' | 'unused';
export type SidebarTabId = 'smart' | 'collections' | 'favourites' | 'scripts' | 'trash';

interface SidebarMemory {
  tab: SidebarTabId;
  smart?: SmartViewId;
  collection?: number;
  favourite?: number;
  script?: string;
}

const TABS: readonly SidebarTabId[] = ['smart', 'collections', 'favourites', 'scripts', 'trash'];
const SMART_VIEW_IDS: readonly SmartViewId[] = ['all', 'recent', 'usage', 'failing', 'unused'];

const SMART_VIEWS: Record<SmartViewId, () => WorkspaceView> = {
  all: () => new AllRequestsView(),
  recent: () => new RecentView(),
  usage: () => new MostUsedView(),
  failing: () => new FailingView(),
  unused: () => new UnusedView(),
};

const storageKey = (projectId: number): string => `snaprq.sidebar.${projectId}`;

/**
 * The sidebar's active tab and the last pick in each tab, kept per project
 * across launches. Picks are read off the open view, so every way of opening
 * a view is remembered.
 */
@Injectable({ providedIn: 'root' })
export class SidebarMemoryService {
  private readonly state = inject(WorkspaceStateService);
  private readonly collectionApi = inject(CollectionApiService);
  private readonly favouriteApi = inject(FavouriteApiService);
  private readonly scriptApi = inject(ScriptApiService);

  readonly tab = signal<SidebarTabId>('collections');
  private memory: SidebarMemory = { tab: 'collections' };

  constructor() {
    effect(() => {
      const view = this.state.view();
      const project = this.state.selectedProject();
      if (!view || !project) return;
      untracked(() => this.remember(project.id, view));
    });
  }

  /** Called once a project's collections, favourites and scripts are loaded. */
  restore(): void {
    const project = this.state.selectedProject();
    if (!project) return;
    this.memory = load(project.id);
    this.openTab(this.memory.tab);
  }

  /** Shows the tab and opens its last pick, or its first item. */
  openTab(tab: SidebarTabId): void {
    const project = this.state.selectedProject();
    if (!project) return;
    this.tab.set(tab);
    switch (tab) {
      case 'smart':
        this.state.selectView(SMART_VIEWS[this.memory.smart ?? 'all']());
        break;
      case 'trash':
        this.state.selectView(new TrashView());
        void this.state.loadBinnedRequests(project.id);
        break;
      case 'collections': {
        const all = this.collectionApi.collections();
        const pick = all.find((c) => c.id === this.memory.collection) ?? all[0];
        this.state.selectView(pick ? new CollectionView(pick) : null);
        break;
      }
      case 'favourites': {
        const all = this.favouriteApi.collections();
        const pick = all.find((f) => f.id === this.memory.favourite) ?? all[0];
        this.state.selectView(pick ? new FavouriteView(pick) : null);
        break;
      }
      case 'scripts': {
        const all = this.scriptApi.allScripts();
        const pick = all.find((s) => s.name === this.memory.script) ?? all[0];
        this.state.selectView(pick ? new ScriptView(pick) : null);
        break;
      }
    }
    this.save(project.id);
  }

  private remember(projectId: number, view: WorkspaceView): void {
    switch (view.kind) {
      case 'collection':
        this.memory.collection = view.collection.id;
        this.tab.set('collections');
        break;
      case 'favourite':
        this.memory.favourite = view.favourite.id;
        this.tab.set('favourites');
        break;
      case 'script':
        this.memory.script = view.name;
        this.tab.set('scripts');
        break;
      case 'bin':
        this.tab.set('trash');
        break;
      default:
        this.memory.smart = view.kind;
        this.tab.set('smart');
    }
    this.save(projectId);
  }

  private save(projectId: number): void {
    this.memory.tab = this.tab();
    try {
      localStorage.setItem(storageKey(projectId), JSON.stringify(this.memory));
    } catch {
      // Storage can be full or disabled; the sidebar still works for this session.
    }
  }
}

function load(projectId: number): SidebarMemory {
  let raw: Partial<SidebarMemory> = {};
  try {
    raw = JSON.parse(localStorage.getItem(storageKey(projectId)) ?? '{}') ?? {};
  } catch {
    raw = {};
  }
  return {
    tab: TABS.includes(raw.tab as SidebarTabId) ? (raw.tab as SidebarTabId) : 'collections',
    smart: SMART_VIEW_IDS.includes(raw.smart as SmartViewId) ? raw.smart : undefined,
    collection: typeof raw.collection === 'number' ? raw.collection : undefined,
    favourite: typeof raw.favourite === 'number' ? raw.favourite : undefined,
    script: typeof raw.script === 'string' ? raw.script : undefined,
  };
}
