/**
 * Which request-list views support user drag-ordering.
 *
 * This is the single source of truth that separates two kinds of views:
 *
 * - User-orderable views (collections, favourites, tags) own a persisted
 *   `user_sort_order` per membership. They render in that order by default,
 *   show drag handles, and persist reorders to the backend.
 * - Non-orderable views (all requests, recent, trash bin) have a natural
 *   order (name / last-used / deleted-at). They never show drag handles and
 *   never write order values.
 *
 * Adding a new view kind requires deciding here, at compile time — the
 * request list reads this map for both handle rendering and drag-start
 * guards, so a stray handle can never enable reordering on a view that has
 * no persisted ordering.
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
