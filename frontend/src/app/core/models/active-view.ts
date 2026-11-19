import type { Collection } from '../services/collection.service';
import type { FavouriteCollection } from '../services/favourite.service';

/**
 * The one view the workspace is showing. Exactly one is active at a time, so
 * states like "the trash bin and a collection at once" cannot be expressed.
 * Switch views with `WorkspaceStateService.selectView`.
 */
export type ActiveView =
  /** Nothing chosen yet (no project, or the chosen group was deleted). */
  | { kind: 'none' }
  | { kind: 'all' }
  | { kind: 'recent' }
  | { kind: 'bin' }
  | { kind: 'collection'; collection: Collection }
  | { kind: 'favourite'; favourite: FavouriteCollection }
  | { kind: 'tag'; name: string };

export type ActiveViewKind = ActiveView['kind'];

/** Views that exist in every project and so survive a project switch. */
export const PROJECT_WIDE_VIEWS: ReadonlySet<ActiveViewKind> = new Set(['all', 'recent', 'bin']);
