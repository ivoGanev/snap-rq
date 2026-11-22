import { describe, expect, it } from 'vitest';
import type { HttpRequestSummary, RequestUsage } from '../models/http';
import {
  arrangeFailing,
  arrangeMostUsed,
  arrangeRecent,
  arrangeUnused,
  RECENT_LIMIT,
  type RequestGroup,
} from './presorted';

// The smart views' grouping, with made-up send times: the day headings and
// "30+ days" can't be seen in a fresh database, so they're pinned here.

/** Sunday 22 Nov 2026, noon, local time. */
const NOW = new Date(2026, 10, 22, 12, 0, 0).getTime();

/** A local time relative to NOW's day, as last_used_at (Unix ns). */
function sentAt(daysAgo: number, hours = 12, minutes = 0, seconds = 0): number {
  const date = new Date(2026, 10, 22 - daysAgo, hours, minutes, seconds);
  return date.getTime() * 1e6;
}

function request(id: number, fields: Partial<HttpRequestSummary> = {}): HttpRequestSummary {
  return {
    id,
    collection_id: 1,
    project_id: 1,
    name: `Request ${id}`,
    url: `https://api.example.com/${id}`,
    method: 'GET',
    status_code: 0,
    response_id: 0,
    user_sort_order: 0,
    user_color: null,
    last_used_at: 0,
    ...fields,
  };
}

/** A request sent at a time, with that send's status. */
function sent(id: number, lastUsedAt: number, status = 200): HttpRequestSummary {
  return request(id, { last_used_at: lastUsedAt, status_code: status, response_id: id * 10 });
}

const shape = (groups: RequestGroup[]) => groups.map((g) => [g.label, g.requests.map((r) => r.id)]);

const context = (usage: Record<number, RequestUsage> = {}) => ({ now: NOW, usage });

describe('Recent', () => {
  it('groups sends by day, newest first, and leaves out requests never sent', () => {
    const groups = arrangeRecent(
      [
        sent(1, sentAt(0, 9)),
        sent(2, sentAt(1, 23, 59)),
        sent(3, sentAt(0, 11)),
        sent(4, sentAt(2, 10)),
        request(5),
      ],
      context(),
    );

    expect(shape(groups).slice(0, 2)).toEqual([
      ['Today', [3, 1]],
      ['Yesterday', [2]],
    ]);
    // Older days are headed by their date (wording depends on the locale).
    expect(groups[2].requests.map((r) => r.id)).toEqual([4]);
    expect(groups[2].label).not.toMatch(/Today|Yesterday/);
    expect(groups[2].label).toContain('20');
  });

  it('splits at local midnight', () => {
    const groups = arrangeRecent(
      [sent(1, sentAt(0, 0, 0, 30)), sent(2, sentAt(1, 23, 59, 59))],
      context(),
    );
    expect(shape(groups)).toEqual([
      ['Today', [1]],
      ['Yesterday', [2]],
    ]);
  });

  it(`shows the last ${RECENT_LIMIT} sends only`, () => {
    const requests = Array.from({ length: RECENT_LIMIT + 10 }, (_, i) => sent(i + 1, sentAt(0, 11, i % 60)));
    const shown = arrangeRecent(requests, context()).flatMap((g) => g.requests);
    expect(shown).toHaveLength(RECENT_LIMIT);
  });
});

describe('Failing', () => {
  it('keeps only errors and 4xx/5xx, newest first', () => {
    const groups = arrangeFailing(
      [
        sent(1, sentAt(0, 8), 200),
        sent(2, sentAt(0, 9), 301),
        sent(3, sentAt(0, 10), 404),
        sent(4, sentAt(0, 11), 500),
        sent(5, sentAt(1, 10), 0), // no response: an error
        request(6), // never sent
      ],
      context(),
    );
    expect(shape(groups)).toEqual([
      ['Today', [4, 3]],
      ['Yesterday', [5]],
    ]);
  });
});

describe('Not used lately', () => {
  it('lists never sent (in creation order), then not sent for 30+ days (longest first)', () => {
    const groups = arrangeUnused(
      [
        request(7),
        request(3),
        sent(10, sentAt(31)),
        sent(11, sentAt(40)),
        sent(12, sentAt(29)), // sent within 30 days: not here
        sent(13, sentAt(0)),
      ],
      context(),
    );
    expect(shape(groups)).toEqual([
      ['Never sent', [3, 7]],
      ['Not sent in 30+ days', [11, 10]],
    ]);
  });

  it('is empty when everything was sent recently', () => {
    expect(arrangeUnused([sent(1, sentAt(1)), sent(2, sentAt(10))], context())).toEqual([]);
  });
});

describe('Most used', () => {
  it('groups by level, most used first, and leaves out requests without usage', () => {
    const usage: Record<number, RequestUsage> = {
      1: { id: 1, score: 7, level: 'high' },
      2: { id: 2, score: 2.5, level: 'medium' },
      3: { id: 3, score: 5.1, level: 'high' },
      4: { id: 4, score: 0.3, level: 'low' },
    };
    const groups = arrangeMostUsed([1, 2, 3, 4, 5].map((id) => sent(id, sentAt(0))), context(usage));
    expect(shape(groups)).toEqual([
      ['High use', [1, 3]],
      ['Medium use', [2]],
      ['Low use', [4]],
    ]);
  });
});
