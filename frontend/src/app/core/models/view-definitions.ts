import type { ActiveViewKind } from './active-view';
import type { HttpRequest } from '../services/request.service';
import type { HttpBinnedRequestSummary } from '../services/bin.service';
import type { InspectorContent } from '../../inspector/inspector-content';

/** What a view's Inspector rule gets to decide from. */
export interface InspectorSelection {
  /** The open live request, if any. */
  request: HttpRequest | null;
  /** The selected row in the trash bin, if any. */
  binned: HttpBinnedRequestSummary | null;
  /** How many rows are selected in the request list. */
  selectedCount: number;
}

/** Everything that is specific to one kind of view. */
export interface ViewDefinition {
  /** Rows can be drag-reordered and the order is saved. */
  orderable: boolean;
  /** Rows can be given a colour, saved per view. */
  colorable: boolean;
  /** What the Inspector shows for the current selection in this view. */
  inspector(selection: InspectorSelection): InspectorContent;
}

const SELECT_A_REQUEST: InspectorContent = {
  kind: 'message',
  text: 'Select a request to view its response.',
};

/** A row is selected but its data is still loading: show nothing yet. */
const NOTHING_YET: InspectorContent = { kind: 'message', text: '' };

/** Views whose rows are live requests: show the open request's responses. */
function requestInspector({ request, selectedCount }: InspectorSelection): InspectorContent {
  if (request) return { kind: 'response', requestId: request.id };
  return selectedCount === 0 ? SELECT_A_REQUEST : NOTHING_YET;
}

/** The trash bin: show the selected binned row. */
function binInspector({ binned, selectedCount }: InspectorSelection): InspectorContent {
  if (binned) return { kind: 'binned-request', binId: binned.id };
  return selectedCount === 0 ? SELECT_A_REQUEST : NOTHING_YET;
}

/**
 * One definition per view kind. Adding a view kind does not compile until it
 * has an entry here.
 */
export const VIEW_DEFINITIONS: { readonly [K in ActiveViewKind]: ViewDefinition } = {
  none: { orderable: false, colorable: false, inspector: requestInspector },
  all: { orderable: false, colorable: false, inspector: requestInspector },
  recent: { orderable: false, colorable: false, inspector: requestInspector },
  bin: { orderable: false, colorable: false, inspector: binInspector },
  collection: { orderable: true, colorable: true, inspector: requestInspector },
  favourite: { orderable: true, colorable: true, inspector: requestInspector },
  tag: { orderable: true, colorable: true, inspector: requestInspector },
};
