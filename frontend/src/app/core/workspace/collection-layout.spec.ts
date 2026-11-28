import { describe, expect, it } from 'vitest';
import type { HttpRequestSummary } from '../models/http';
import type { RequestGroupRecord } from '../services/group.service';
import {
  arrangeCollection,
  flattenLayout,
  groupDropTarget,
  moveGroup,
  moveRequests,
  requestDropTarget,
  sameLayout,
  type LayoutItem,
} from './collection-layout';
import { toEntries, type ListEntry, type ListSection } from './list-entries';

// Where drops land in a collection's layout. Rows are 10px tall here, so a
// pointer at y = 25 is halfway down the third row.

const H = 10;

function req(id: number, name: string, order: number, groupId: number | null = null): HttpRequestSummary {
  return {
    id,
    collection_id: 1,
    project_id: 1,
    name,
    url: '',
    method: 'GET',
    status_code: 0,
    response_id: 0,
    user_sort_order: order,
    user_color: null,
    last_used_at: 0,
    group_id: groupId,
  };
}

function group(id: number, name: string, order: number): RequestGroupRecord {
  return { id, collection_id: 1, name, color: null, sort_order: order };
}

/** alpha, [Auth: bravo, charlie], delta */
const AUTH = group(10, 'Auth', 1);
const REQUESTS = [req(1, 'alpha', 0), req(2, 'bravo', 1, 10), req(3, 'charlie', 2, 10), req(4, 'delta', 3)];

function rows(collapsed = false, groups = [AUTH], requests = REQUESTS): ListEntry[] {
  const sections: ListSection[] = [];
  for (const item of arrangeCollection(groups, requests)) {
    if (item.kind === 'request') {
      const last = sections[sections.length - 1];
      if (last && last.heading === null) last.requests.push(item.req);
      else sections.push({ heading: null, requests: [item.req] });
    } else {
      sections.push({
        heading: { key: `group:${item.group.id}`, label: item.group.name, color: null, groupId: item.group.id },
        requests: item.members,
      });
    }
  }
  return toEntries(sections, () => collapsed);
}

const label = (r: ListEntry) => (r.kind === 'group' ? `# ${r.label}` : r.depth ? `  ${r.req.name}` : r.req.name);
const LAYOUT = flattenLayout(arrangeCollection([AUTH], REQUESTS));
const names = (layout: LayoutItem[]) =>
  layout.map((it) =>
    it.kind === 'group' ? '# Auth' : `${it.groupId ? '  ' : ''}${REQUESTS.find((r) => r.id === it.id)!.name}`,
  );

describe('arrangeCollection', () => {
  it('lays requests and groups out by their shared order, a group followed by its requests', () => {
    expect(rows().map(label)).toEqual(['alpha', '# Auth', '  bravo', '  charlie', 'delta']);
  });

  it('breaks ties by name and treats a request of a missing group as ungrouped', () => {
    const laid = arrangeCollection(
      [group(10, 'Beta', 0)],
      [req(1, 'zulu', 0), req(2, 'alpha', 0), req(3, 'orphan', 5, 99)],
    );
    expect(laid.map((it) => (it.kind === 'group' ? `# ${it.group.name}` : it.req.name))).toEqual([
      'alpha',
      '# Beta',
      'zulu',
      'orphan',
    ]);
  });
});

describe('toEntries', () => {
  it('marks nesting, the last request of a group and the zebra stripe by place in the list', () => {
    const r = rows();
    expect(r.map((e) => (e.kind === 'request' ? [e.depth, e.last, e.even] : 'heading'))).toEqual([
      [0, false, false],
      'heading',
      [1, false, false],
      [1, true, true],
      [0, false, false],
    ]);
  });

  it('keeps a folded group’s heading, with its count, and hides its requests', () => {
    const r = rows(true);
    expect(r.map(label)).toEqual(['alpha', '# Auth', 'delta']);
    expect(r[1]).toMatchObject({ kind: 'group', count: 2, collapsed: true });
  });
});

describe('requestDropTarget', () => {
  it('drops onto a heading’s middle into that group, with no line', () => {
    const t = requestDropTarget(rows(), 15, H, true);
    expect(t).toMatchObject({ into: 10, parent: 10, lineRow: null, anchor: { kind: 'groupEnd', groupId: 10 } });
  });

  it('drops at a heading’s top edge before the group, outside it', () => {
    const t = requestDropTarget(rows(), 11, H, true);
    expect(t).toMatchObject({ parent: null, lineRow: 1, indent: false, anchor: { kind: 'before', item: { kind: 'group', id: 10 } } });
  });

  it('drops between two of a group’s requests into the group, with an indented line', () => {
    const t = requestDropTarget(rows(), 30, H, false);
    expect(t).toMatchObject({ parent: 10, lineRow: 3, indent: true, anchor: { kind: 'before', item: { kind: 'request', id: 3 } } });
  });

  it('at a group’s last request, keeps the drop inside when the pointer is inside, else after the group', () => {
    expect(requestDropTarget(rows(), 40, H, true)).toMatchObject({ parent: 10, anchor: { kind: 'groupEnd', groupId: 10 } });
    expect(requestDropTarget(rows(), 40, H, false)).toMatchObject({
      parent: null,
      anchor: { kind: 'before', item: { kind: 'request', id: 4 } },
    });
  });

  it('drops below an open empty group’s heading into it, but not below a folded one', () => {
    const empty = group(20, 'Empty', 1);
    const requests = [req(1, 'alpha', 0), req(4, 'delta', 3)];
    expect(requestDropTarget(rows(false, [empty], requests), 20, H, true)).toMatchObject({ parent: 20 });
    expect(requestDropTarget(rows(true, [AUTH]), 20, H, true)).toMatchObject({
      parent: null,
      anchor: { kind: 'before', item: { kind: 'request', id: 4 } },
    });
  });

  it('drops past the last row at the end', () => {
    expect(requestDropTarget(rows(), 500, H, true)).toMatchObject({ parent: null, lineRow: 5, anchor: { kind: 'end' } });
  });
});

describe('groupDropTarget', () => {
  it('never lands inside a group: inside one it snaps before or after it', () => {
    expect(groupDropTarget(rows(), 20, H)).toMatchObject({ lineRow: 1, anchor: { kind: 'before', item: { kind: 'group', id: 10 } } });
    expect(groupDropTarget(rows(), 30, H)).toMatchObject({ lineRow: 4, anchor: { kind: 'groupEnd', groupId: 10 } });
  });

  it('lands between top-level items as they are', () => {
    expect(groupDropTarget(rows(), 10, H)).toMatchObject({ parent: null, lineRow: 1 });
    expect(groupDropTarget(rows(), 500, H)).toMatchObject({ anchor: { kind: 'end' } });
  });
});

describe('moveRequests', () => {
  it('moves a request into a group at the line', () => {
    const t = requestDropTarget(rows(), 30, H, false);
    expect(names(moveRequests(LAYOUT, new Set([4]), t))).toEqual(['alpha', '# Auth', '  bravo', '  delta', '  charlie']);
  });

  it('moves several requests together, keeping their order, out of a group', () => {
    const t = requestDropTarget(rows(), 0, H, false);
    expect(names(moveRequests(LAYOUT, new Set([3, 2]), t))).toEqual(['bravo', 'charlie', 'alpha', '# Auth', 'delta']);
  });

  it('is a no-op when dropped right where the requests already are', () => {
    const t = requestDropTarget(rows(), 30, H, false);
    expect(sameLayout(moveRequests(LAYOUT, new Set([2]), t), LAYOUT)).toBe(true);
    expect(sameLayout(moveRequests(LAYOUT, new Set([4]), t), LAYOUT)).toBe(false);
  });
});

describe('moveGroup', () => {
  it('moves a group with its requests', () => {
    const t = groupDropTarget(rows(), 500, H);
    expect(names(moveGroup(LAYOUT, 10, t))).toEqual(['alpha', 'delta', '# Auth', '  bravo', '  charlie']);
  });

  it('is a no-op when dropped just after itself', () => {
    const t = groupDropTarget(rows(), 30, H);
    expect(sameLayout(moveGroup(LAYOUT, 10, t), LAYOUT)).toBe(true);
  });
});
