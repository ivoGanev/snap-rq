import type { Collection } from '../services/collection.service';
import type { FavouriteCollection } from '../services/favourite.service';
import type { Script } from '../services/script.service';
import type { HttpRequest } from '../services/request.service';
import type { HttpBinnedRequestSummary } from '../services/bin.service';
import type { InspectorContent } from '../../inspector/inspector';

/**
 * The views the workspace can show: all requests, recent, the trash bin, or
 * the requests of one collection, favourite list or script. Each view is one
 * small object that knows everything specific to it: its title, what the user
 * may do with its rows, and what the Inspector shows for a selection.
 *
 * The open view lives in `WorkspaceStateService.view`; switch it with
 * `selectView(new TrashView())` etc.
 */
export type WorkspaceView =
  | AllRequestsView
  | RecentView
  | TrashView
  | CollectionView
  | FavouriteView
  | ScriptView;

export type WorkspaceViewKind = WorkspaceView['kind'];

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
  /** Shown above the request list. */
  readonly title: string;
  /** Rows can be drag-reordered and the order is saved for this view. */
  readonly canReorder: boolean;
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
  readonly title = 'All requests';
  readonly canReorder = false;
  readonly canColour = false;
  readonly survivesProjectSwitch = true;
  inspect = inspectRequest;
}

export class RecentView implements ViewBehaviour {
  readonly kind = 'recent';
  readonly title = 'Recent';
  readonly canReorder = false;
  readonly canColour = false;
  readonly survivesProjectSwitch = true;
  inspect = inspectRequest;
}

export class TrashView implements ViewBehaviour {
  readonly kind = 'bin';
  readonly title = 'Trash bin';
  readonly canReorder = false;
  readonly canColour = false;
  readonly survivesProjectSwitch = true;

  inspect({ binned, selectedCount }: InspectorSelection): InspectorContent {
    if (binned) return { kind: 'binned-request', binId: binned.id };
    return selectedCount === 0 ? SELECT_A_REQUEST : NOTHING_YET;
  }
}

export class CollectionView implements ViewBehaviour {
  readonly kind = 'collection';
  readonly canReorder = true;
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
  readonly canReorder = true;
  readonly canColour = true;
  readonly survivesProjectSwitch = false;
  inspect = inspectRequest;

  constructor(readonly favourite: FavouriteCollection) {}

  get title(): string {
    return this.favourite.name;
  }
}

export class ScriptView implements ViewBehaviour {
  readonly kind = 'script';
  readonly canReorder = true;
  readonly canColour = true;
  readonly survivesProjectSwitch = false;

  constructor(readonly script: Script) {}

  get name(): string {
    return this.script.name;
  }

  get title(): string {
    return this.script.name;
  }

  /** Always the script's editor, even when a request in it is selected. */
  inspect(): InspectorContent {
    return { kind: 'script', scriptId: this.script.id };
  }
}
