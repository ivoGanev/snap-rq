import type { HttpRequestSummary } from '../models/http';
import { httpStatusClass } from '../http-status';
import { dayLabel, fromUnixNano } from '../time';

/**
 * The smart views' arrangements: each picks and orders a project's requests
 * and groups them under headings (a day, "Never sent"…). Pure functions over
 * the request list, so the views need no backend query of their own.
 */

/** Some of a view's requests under one heading, in display order. */
export interface RequestGroup {
  key: string;
  label: string;
  requests: HttpRequestSummary[];
}

/** What arranging may depend on besides the requests. */
export interface ArrangeContext {
  /** The current time (ms), for day headings and "30+ days". */
  now: number;
}

/** How many requests Recent shows (as it always has). */
export const RECENT_LIMIT = 50;

/** After how many days without a send a request counts as not used lately. */
export const UNUSED_AFTER_DAYS = 30;

/** The result of a request's last send, for the timeline's dot colour. */
export type LastResult = 'never' | 'error' | '2xx' | '3xx' | '4xx' | '5xx';

export function lastResult(req: HttpRequestSummary): LastResult {
  if (req.response_id === 0) return 'never';
  if (req.status_code === 0) return 'error';
  return httpStatusClass(req.status_code);
}

const byLastSentDesc = (a: HttpRequestSummary, b: HttpRequestSummary) =>
  b.last_used_at - a.last_used_at || a.name.localeCompare(b.name);

/** Requests by the day they were last sent, newest first. */
function byDay(requests: HttpRequestSummary[], now: number): RequestGroup[] {
  const groups: RequestGroup[] = [];
  for (const req of requests) {
    const label = dayLabel(fromUnixNano(req.last_used_at) ?? new Date(NaN), now);
    const last = groups.at(-1);
    if (last?.label === label) last.requests.push(req);
    else groups.push({ key: label, label, requests: [req] });
  }
  return groups;
}

/** Recent: the last sent requests, newest first, by day. */
export function arrangeRecent(requests: HttpRequestSummary[], { now }: ArrangeContext): RequestGroup[] {
  const sent = requests.filter((r) => r.last_used_at > 0).sort(byLastSentDesc);
  return byDay(sent.slice(0, RECENT_LIMIT), now);
}

/** Failing: last send got an error or a 4xx/5xx, newest first, by day. */
export function arrangeFailing(requests: HttpRequestSummary[], { now }: ArrangeContext): RequestGroup[] {
  const failing = requests
    .filter((r) => {
      const result = lastResult(r);
      return result === 'error' || result === '4xx' || result === '5xx';
    })
    .sort(byLastSentDesc);
  return byDay(failing, now);
}

/**
 * Not used lately: never sent (in the order they were created), then not
 * sent for UNUSED_AFTER_DAYS (the longest unused first).
 */
export function arrangeUnused(requests: HttpRequestSummary[], { now }: ArrangeContext): RequestGroup[] {
  const cutoffNanos = (now - UNUSED_AFTER_DAYS * 86_400_000) * 1e6;
  const never = requests.filter((r) => r.last_used_at === 0).sort((a, b) => a.id - b.id);
  const stale = requests
    .filter((r) => r.last_used_at > 0 && r.last_used_at < cutoffNanos)
    .sort((a, b) => a.last_used_at - b.last_used_at || a.name.localeCompare(b.name));
  const groups: RequestGroup[] = [];
  if (never.length) groups.push({ key: 'never', label: 'Never sent', requests: never });
  if (stale.length) {
    groups.push({ key: 'stale', label: `Not sent in ${UNUSED_AFTER_DAYS}+ days`, requests: stale });
  }
  return groups;
}
