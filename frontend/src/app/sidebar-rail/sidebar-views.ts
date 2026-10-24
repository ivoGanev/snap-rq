import type { SidebarFolder } from '../request-group-list/request-group-list';

export type SidebarViewId = 'all' | 'famous' | 'collections' | 'favourites' | 'tags' | 'trash';

export interface SidebarView {
  id: SidebarViewId;
  label: string;
  hint: string;
  iconPath: string;
  folder?: SidebarFolder;
  dividerBefore?: boolean;
}

/** Single source of truth for the sidebar navigation entries. */
export const SIDEBAR_VIEWS: readonly SidebarView[] = [
  {
    id: 'all',
    label: 'All requests',
    hint: 'All requests',
    iconPath: 'M4 6h16v2H4zm0 5h16v2H4zm0 5h16v2H4z',
  },
  {
    id: 'famous',
    label: 'Recent',
    hint: 'Recent requests',
    iconPath:
      'M11.99 2C6.47 2 2 6.48 2 12s4.47 10 9.99 10C17.52 22 22 17.52 22 12S17.52 2 11.99 2zM12 20c-4.42 0-8-3.58-8-8s3.58-8 8-8 8 3.58 8 8-3.58 8-8 8zm.5-13H11v6l5.25 3.15.75-1.23-4.5-2.67z',
  },
  {
    id: 'collections',
    label: 'Collections',
    hint: 'Collections',
    iconPath: 'M10 4H4a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-8l-2-2z',
    folder: 'collections',
  },
  {
    id: 'favourites',
    label: 'Favourites',
    hint: 'Favourites',
    iconPath:
      'M12 21.35l-1.45-1.32C5.4 15.36 2 12.28 2 8.5 2 5.42 4.42 3 7.5 3c1.74 0 3.41.81 4.5 2.09C13.09 3.81 14.76 3 16.5 3 19.58 3 22 5.42 22 8.5c0 3.78-3.4 6.86-8.55 11.54L12 21.35z',
    folder: 'favourites',
  },
  {
    id: 'tags',
    label: 'Tags',
    hint: 'Tags',
    iconPath: 'M20.59 13.41l-7.17 7.17a2 2 0 0 1-2.83 0L2 12V2h10l8.59 8.59a2 2 0 0 1 0 2.82z',
    folder: 'tags',
  },
  {
    id: 'trash',
    label: 'Trash bin',
    hint: 'Trash bin',
    iconPath: 'M6 19c0 1.1.9 2 2 2h8c1.1 0 2-.9 2-2V7H6v12zM19 4h-3.5l-1-1h-5l-1 1H5v2h14V4z',
    dividerBefore: true,
  },
];
