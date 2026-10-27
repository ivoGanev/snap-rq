/**
 * Which request-list views support each user-editable capability.
 *
 * This is the single source of truth that separates user-orderable/colour-able
 * views from natural-order views:
 *
 * - User-orderable/colour-able views (collections, favourites, tags) own a
 *   persisted `user_sort_order` / `user_color` per membership.
 * - Non-editable views (all requests, recent, trash bin) have a natural order
 *   and never show ordering or colour controls.
 *
 * The request list reads these maps for both control rendering and drag/apply
 * guards, so a stray handle can never enable a capability on a view that has
 * no persisted state for it.
 */
export type RequestListViewKind = 'collection' | 'favourite' | 'tag' | 'all' | 'recent' | 'bin';

export const USER_ORDERABLE_VIEWS: Readonly<Record<RequestListViewKind, boolean>> = {
  collection: true,
  favourite: true,
  tag: true,
  all: false,
  recent: false,
  bin: false,
};

export const USER_COLORABLE_VIEWS: Readonly<Record<RequestListViewKind, boolean>> = {
  collection: true,
  favourite: true,
  tag: true,
  all: false,
  recent: false,
  bin: false,
};
