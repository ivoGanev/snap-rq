import type { HttpRequestSummary } from '../models/http';
import type { RequestGroupRecord } from '../services/group.service';
import type { ListEntry } from './list-entries';

/**
 * A collection's layout: its requests and groups share one order you set by
 * dragging, and each group holds its requests in their own order.
 */
export type TopItem =
  | { kind: 'request'; req: HttpRequestSummary }
  | { kind: 'group'; group: RequestGroupRecord; members: HttpRequestSummary[] };

const byOrder = (a: HttpRequestSummary, b: HttpRequestSummary) =>
  a.user_sort_order - b.user_sort_order || a.name.localeCompare(b.name);

export function arrangeCollection(groups: RequestGroupRecord[], requests: HttpRequestSummary[]): TopItem[] {
  const members = new Map<number, HttpRequestSummary[]>(groups.map((g) => [g.id, []]));
  const loose: HttpRequestSummary[] = [];
  for (const req of requests) {
    const list = req.group_id != null ? members.get(req.group_id) : undefined;
    (list ?? loose).push(req);
  }
  const items: { order: number; name: string; item: TopItem }[] = [
    ...loose.map((req) => ({ order: req.user_sort_order, name: req.name, item: { kind: 'request', req } as TopItem })),
    ...groups.map((group) => ({
      order: group.sort_order,
      name: group.name,
      item: { kind: 'group', group, members: members.get(group.id)!.sort(byOrder) } as TopItem,
    })),
  ];
  return items.sort((a, b) => a.order - b.order || a.name.localeCompare(b.name)).map((x) => x.item);
}

/** The layout flattened in display order; a group's requests follow it. */
export type LayoutItem = { kind: 'group'; id: number } | { kind: 'request'; id: number; groupId: number | null };

export function flattenLayout(items: TopItem[]): LayoutItem[] {
  const out: LayoutItem[] = [];
  for (const item of items) {
    if (item.kind === 'request') {
      out.push({ kind: 'request', id: item.req.id, groupId: null });
      continue;
    }
    out.push({ kind: 'group', id: item.group.id });
    for (const req of item.members) out.push({ kind: 'request', id: req.id, groupId: item.group.id });
  }
  return out;
}

/** Where dropped items go: before an item, after a group's requests, or at the end. */
export type Anchor =
  | { kind: 'before'; item: { kind: 'group' | 'request'; id: number } }
  | { kind: 'groupEnd'; groupId: number }
  | { kind: 'end' };

export interface DropTarget {
  anchor: Anchor;
  /** The group the dropped requests join; null = none. */
  parent: number | null;
  /** The row boundary the insertion line is drawn at; null when dropping onto a heading. */
  lineRow: number | null;
  /** The line is indented when the drop lands inside a group. */
  indent: boolean;
  /** A heading the requests are dropped onto (they go to the end of its group). */
  into: number | null;
}

const groupOfRow = (row: ListEntry | undefined): number | null =>
  row?.kind === 'request' && row.depth === 1 ? Number(row.section.slice('group:'.length)) : null;

const itemOfRow = (row: ListEntry): { kind: 'group' | 'request'; id: number } =>
  row.kind === 'group' ? { kind: 'group', id: row.groupId! } : { kind: 'request', id: row.req.id };

/**
 * Where requests dragged to content position `y` land. A heading's middle
 * drops into its group; otherwise the nearest row boundary decides, and at
 * a group's last row the pointer's horizontal position picks inside the
 * group (`inside`) or after it.
 */
export function requestDropTarget(rows: ListEntry[], y: number, rowHeight: number, inside: boolean): DropTarget {
  const index = Math.floor(y / rowHeight);
  const row = rows[index];
  const fraction = y / rowHeight - index;
  if (row?.kind === 'group' && row.groupId !== null && fraction > 0.3 && fraction < 0.7) {
    return { anchor: { kind: 'groupEnd', groupId: row.groupId }, parent: row.groupId, lineRow: null, indent: false, into: row.groupId };
  }

  const k = Math.max(0, Math.min(Math.round(y / rowHeight), rows.length));
  const above = rows[k - 1];
  const below = rows[k];
  const belowGroup = groupOfRow(below);
  const aboveGroup = groupOfRow(above);
  const target = (anchor: Anchor, parent: number | null): DropTarget => ({
    anchor,
    parent,
    lineRow: k,
    indent: parent !== null,
    into: null,
  });

  if (below && belowGroup !== null) return target({ kind: 'before', item: itemOfRow(below) }, belowGroup);
  if (above?.kind === 'group' && above.groupId !== null && !above.collapsed) {
    return target({ kind: 'groupEnd', groupId: above.groupId }, above.groupId);
  }
  if (aboveGroup !== null && inside) return target({ kind: 'groupEnd', groupId: aboveGroup }, aboveGroup);
  return target(below ? { kind: 'before', item: itemOfRow(below) } : { kind: 'end' }, null);
}

/** Where a dragged group lands: only between top-level items, never inside another group. */
export function groupDropTarget(rows: ListEntry[], y: number, rowHeight: number): DropTarget {
  const k = Math.max(0, Math.min(Math.round(y / rowHeight), rows.length));
  const above = rows[k - 1];
  const below = rows[k];
  const belowGroup = groupOfRow(below);
  if (belowGroup !== null) {
    if (above?.kind === 'group' && above.groupId === belowGroup) {
      return { anchor: { kind: 'before', item: { kind: 'group', id: belowGroup } }, parent: null, lineRow: k - 1, indent: false, into: null };
    }
    let end = k;
    while (groupOfRow(rows[end]) === belowGroup) end++;
    return { anchor: { kind: 'groupEnd', groupId: belowGroup }, parent: null, lineRow: end, indent: false, into: null };
  }
  const anchor: Anchor = below ? { kind: 'before', item: itemOfRow(below) } : { kind: 'end' };
  return { anchor, parent: null, lineRow: k, indent: false, into: null };
}

function resolve(layout: LayoutItem[], anchor: Anchor): number {
  if (anchor.kind === 'end') return layout.length;
  if (anchor.kind === 'before') {
    const at = layout.findIndex((it) => it.kind === anchor.item.kind && it.id === anchor.item.id);
    return at === -1 ? layout.length : at;
  }
  let at = layout.findIndex((it) => it.kind === 'group' && it.id === anchor.groupId);
  if (at === -1) return layout.length;
  while (layout[at + 1]?.kind === 'request' && (layout[at + 1] as { groupId: number | null }).groupId === anchor.groupId) at++;
  return at + 1;
}

/** Moves the requests, in their current order, to the target. */
export function moveRequests(layout: LayoutItem[], ids: Set<number>, target: DropTarget): LayoutItem[] {
  const at = resolve(layout, target.anchor);
  const isMoving = (it: LayoutItem) => it.kind === 'request' && ids.has(it.id);
  const moving = layout
    .filter(isMoving)
    .map((it): LayoutItem => ({ kind: 'request', id: it.id, groupId: target.parent }));
  return [...layout.slice(0, at).filter((it) => !isMoving(it)), ...moving, ...layout.slice(at).filter((it) => !isMoving(it))];
}

/** Moves a group with its requests to the target. */
export function moveGroup(layout: LayoutItem[], groupId: number, target: DropTarget): LayoutItem[] {
  const inBlock = (it: LayoutItem) =>
    (it.kind === 'group' && it.id === groupId) || (it.kind === 'request' && it.groupId === groupId);
  const at = resolve(layout, target.anchor);
  const block = layout.filter(inBlock);
  return [...layout.slice(0, at).filter((it) => !inBlock(it)), ...block, ...layout.slice(at).filter((it) => !inBlock(it))];
}

export function sameLayout(a: LayoutItem[], b: LayoutItem[]): boolean {
  return (
    a.length === b.length &&
    a.every((it, i) => {
      const other = b[i];
      if (it.kind !== other.kind || it.id !== other.id) return false;
      return it.kind === 'group' || it.groupId === (other as { groupId: number | null }).groupId;
    })
  );
}
