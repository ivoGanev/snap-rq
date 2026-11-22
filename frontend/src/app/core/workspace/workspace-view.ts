import type { Collection } from '../services/collection.service';
import type { FavouriteCollection } from '../services/favourite.service';
import type { Script } from '../services/script.service';
import type { HttpRequest } from '../services/request.service';
import type { HttpBinnedRequestSummary } from '../services/bin.service';
import type { InspectorContent } from '../../inspector/inspector';
import type { HttpRequestSummary } from '../models/http';
import {
  arrangeFailing,
  arrangeRecent,
  arrangeUnused,
  type ArrangeContext,
  type RequestGroup,
} from './presorted';

/**
 * The views the workspace can show: all requests, the smart views (Recent,
 * Failing, Not used lately), the trash bin, or the requests of one
 * collection, favourite list or script. Each view is one
 * small object that knows everything specific to it: its title, what the user
 * may do with its rows, and what the Inspector shows for a selection.
 *
 * The open view lives in `WorkspaceStateService.view`; switch it with
 * `selectView(new TrashView())` etc.
 */
export type WorkspaceView =
  | AllRequestsView
  | RecentView
  | FailingView
  | UnusedView
  | TrashView
  | CollectionView
  | FavouriteView
  | ScriptView;

export type WorkspaceViewKind = WorkspaceView['kind'];

/** What the middle column shows: the request list, or a script's editor. */
export type MainColumn = 'requests' | 'script';

/** What a view decides the Inspector content from. */
export interface InspectorSelection {
  /** The open live request, if any. */
  request: HttpRequest | null;
  /** The selected row in the trash bin, if any. */
  binned: HttpBinnedRequestSummary | null;
  /** How many rows are selected in the request list. */
  selectedCount: number;
}

interface ViewBehaviour {
  /** What the middle column shows for this view. */
  readonly main: MainColumn;
  /** Shown above the request list. */
  readonly title: string;
  /** Rows can be drag-reordered and the order is saved for this view. */
  readonly canReorder: boolean;
  /** The column headers sort the rows (smart views keep their own order). */
  readonly sortable: boolean;
  /** Rows can be given a colour, saved for this view. */
  readonly canColour: boolean;
  /** Stays open when the user switches project (it exists in every project). */
  readonly survivesProjectSwitch: boolean;
  /** What the Inspector shows for the current selection. */
  inspect(selection: InspectorSelection): InspectorContent;
}

const SELECT_A_REQUEST: InspectorContent = {
  kind: 'message',
  text: 'Select a request to view its response.',
};

/** A row is selected but its data is still loading: show nothing yet. */
const NOTHING_YET: InspectorContent = { kind: 'message', text: '' };

/**
 * Views whose rows are live requests show the open request's responses. Also
 * used when no view is open, since a request can still be open then.
 */
export function inspectRequest({ request, selectedCount }: InspectorSelection): InspectorContent {
  if (request) return { kind: 'response', requestId: request.id };
  return selectedCount === 0 ? SELECT_A_REQUEST : NOTHING_YET;
}

export class AllRequestsView implements ViewBehaviour {
  readonly kind = 'all';
  readonly main = 'requests';
  readonly title = 'All requests';
  readonly canReorder = false;
  readonly sortable = true;
  readonly canColour = false;
  readonly survivesProjectSwitch = true;
  inspect = inspectRequest;
}

/**
 * A smart view: the project's requests in an order the view picks, grouped
 * under headings (a day, "Never sent"…) and shown as a timeline. The order
 * is the point, so it can't be sorted, reordered or coloured.
 */
abstract class SmartViewBase implements ViewBehaviour {
  readonly main = 'requests';
  readonly canReorder = false;
  readonly canColour = false;
  readonly sortable = false;
  readonly survivesProjectSwitch = true;
  inspect = inspectRequest;

  abstract readonly title: string;
  /** Shown when the view has no requests. */
  abstract readonly emptyText: string;
  /** Picks, orders and groups the project's requests. */
  abstract arrange(requests: HttpRequestSummary[], context: ArrangeContext): RequestGroup[];
}

/** Recent: the last sent requests, newest first, by day. */
export class RecentView extends SmartViewBase {
  readonly kind = 'recent';
  readonly title = 'Recent';
  readonly emptyText = 'Requests you send show up here, newest first.';
  arrange = arrangeRecent;
}

/** Failing: requests whose last send got an error or a 4xx/5xx. */
export class FailingView extends SmartViewBase {
  readonly kind = 'failing';
  readonly title = 'Failing';
  readonly emptyText = 'Nothing is failing. Requests whose last send got an error or a 4xx/5xx show up here.';
  arrange = arrangeFailing;
}

/** Not used lately: never sent, or not sent for a month. */
export class UnusedView extends SmartViewBase {
  readonly kind = 'unused';
  readonly title = 'Not used lately';
  readonly emptyText = 'Every request has been sent recently.';
  arrange = arrangeUnused;
}

/** The smart views (see SmartViewBase). */
export type SmartView = RecentView | FailingView | UnusedView;

export function isSmartView(view: WorkspaceView | null): view is SmartView {
  return view instanceof SmartViewBase;
}

export class TrashView implements ViewBehaviour {
  readonly kind = 'bin';
  readonly main = 'requests';
  readonly title = 'Trash bin';
  readonly canReorder = false;
  readonly sortable = true;
  readonly canColour = false;
  readonly survivesProjectSwitch = true;

  inspect({ binned, selectedCount }: InspectorSelection): InspectorContent {
    if (binned) return { kind: 'binned-request', binId: binned.id };
    return selectedCount === 0 ? SELECT_A_REQUEST : NOTHING_YET;
  }
}

export class CollectionView implements ViewBehaviour {
  readonly kind = 'collection';
  readonly main = 'requests';
  readonly canReorder = true;
  readonly sortable = true;
  readonly canColour = true;
  readonly survivesProjectSwitch = false;
  inspect = inspectRequest;

  constructor(readonly collection: Collection) {}

  get title(): string {
    return this.collection.name;
  }
}

export class FavouriteView implements ViewBehaviour {
  readonly kind = 'favourite';
  readonly main = 'requests';
  readonly canReorder = true;
  readonly sortable = true;
  readonly canColour = true;
  readonly survivesProjectSwitch = false;
  inspect = inspectRequest;

  constructor(readonly favourite: FavouriteCollection) {}

  get title(): string {
    return this.favourite.name;
  }
}

/** The two tabs of a script view: its code, or the requests it is attached to. */
export type ScriptTab = 'script' | 'attachments';

export class ScriptView implements ViewBehaviour {
  readonly kind = 'script';
  readonly canReorder = true;
  readonly sortable = true;
  readonly canColour = true;
  readonly survivesProjectSwitch = false;

  constructor(
    readonly script: Script,
    readonly tab: ScriptTab = 'script',
  ) {}

  /** The editor on the Script tab, the request list on the Attachments tab. */
  get main(): MainColumn {
    return this.tab === 'script' ? 'script' : 'requests';
  }

  get name(): string {
    return this.script.name;
  }

  get title(): string {
    return this.script.name;
  }

  /** On both tabs: the Inspector tries the script (and has a reference). */
  inspect(): InspectorContent {
    return { kind: 'script', script: this.script };
  }
}
