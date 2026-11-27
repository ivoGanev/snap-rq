import type { HttpRequestSummary } from '../models/http';

/**
 * One drawn row of the request list. Every kind is drawn at the same height,
 * so virtual scrolling keeps working; new kinds (a comment, a wait) add a
 * member here and a template block, and nothing else in the list changes.
 */
export type ListEntry = GroupEntry | RequestEntry;

/** A heading over some requests: a collection's group, or a smart view's bucket. */
export interface GroupEntry {
  kind: 'group';
  /** Stable within the list; also the key its folded state is saved under. */
  key: string;
  label: string;
  count: number;
  /** The group's colour as CSS, or null. */
  color: string | null;
  collapsed: boolean;
  /** The stored group's id; null for a smart view's bucket, which can't be edited. */
  groupId: number | null;
}

export interface RequestEntry {
  kind: 'request';
  req: HttpRequestSummary;
}

/** Some of a list's requests, with the heading drawn above them (none for the ungrouped rest). */
export interface ListSection {
  heading: Omit<GroupEntry, 'kind' | 'count' | 'collapsed'> | null;
  requests: HttpRequestSummary[];
}

/** Lays sections out as rows; a folded section keeps its heading and hides its requests. */
export function toEntries(sections: ListSection[], isCollapsed: (key: string) => boolean): ListEntry[] {
  const rows: ListEntry[] = [];
  for (const section of sections) {
    const collapsed = section.heading ? isCollapsed(section.heading.key) : false;
    if (section.heading) {
      rows.push({ kind: 'group', ...section.heading, count: section.requests.length, collapsed });
    }
    if (collapsed) continue;
    for (const req of section.requests) rows.push({ kind: 'request', req });
  }
  return rows;
}
