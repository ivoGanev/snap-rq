/**
 * Which request-list views support each user-editable capability, read from
 * the view definitions (core/models/view-definitions.ts), which are the
 * single source of truth.
 *
 * - User-orderable/colour-able views (collections, favourites, tags) own a
 *   persisted `user_sort_order` / `user_color` per membership.
 * - Non-editable views (all requests, recent, trash bin) have a natural order
 *   and never show ordering or colour controls.
 *
 * The request list reads these maps for both control rendering and drag/apply
 * guards, so a stray handle can never enable a capability on a view that has
 * no persisted state for it.
 *
 * Step A3 of docs/plans/tags-to-scripts.md moves the request list onto the
 * view definitions directly and removes this file.
 */
import type { ActiveViewKind } from '../core/models/active-view';
import { VIEW_DEFINITIONS } from '../core/models/view-definitions';

export type RequestListViewKind = Exclude<ActiveViewKind, 'none'>;

function capabilityMap(capability: 'orderable' | 'colorable'): Readonly<Record<RequestListViewKind, boolean>> {
  return {
    collection: VIEW_DEFINITIONS.collection[capability],
    favourite: VIEW_DEFINITIONS.favourite[capability],
    tag: VIEW_DEFINITIONS.tag[capability],
    all: VIEW_DEFINITIONS.all[capability],
    recent: VIEW_DEFINITIONS.recent[capability],
    bin: VIEW_DEFINITIONS.bin[capability],
  };
}

export const USER_ORDERABLE_VIEWS = capabilityMap('orderable');
export const USER_COLORABLE_VIEWS = capabilityMap('colorable');
