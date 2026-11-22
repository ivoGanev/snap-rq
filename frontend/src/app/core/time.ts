/**
 * Date helpers for timelines (response history, Recent, Failing): day
 * headings, clock times and "2 min ago". All in the user's local time.
 */

/** A saved response's time: UTC, as "YYYY-MM-DD HH:MM:SS" or ISO. */
export function parseSavedTime(createdAt: string): Date {
  const iso = createdAt.includes('T') ? createdAt : createdAt.replace(' ', 'T') + 'Z';
  return new Date(iso);
}

/** A request's last_used_at (Unix nanoseconds, 0 = never) as a Date, or null. */
export function fromUnixNano(nanos: number): Date | null {
  return nanos > 0 ? new Date(nanos / 1e6) : null;
}

export function clockTime(date: Date): string {
  if (Number.isNaN(date.getTime())) return '';
  return date.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
}

function startOfDay(time: number): number {
  const date = new Date(time);
  date.setHours(0, 0, 0, 0);
  return date.getTime();
}

/** Today, Yesterday, or the date ("Sat, Nov 21"). */
export function dayLabel(date: Date, now: number): string {
  if (Number.isNaN(date.getTime())) return 'Earlier';
  const days = Math.round((startOfDay(now) - startOfDay(date.getTime())) / 86_400_000);
  if (days === 0) return 'Today';
  if (days === 1) return 'Yesterday';
  return date.toLocaleDateString(undefined, { weekday: 'short', month: 'short', day: 'numeric' });
}

/** "just now", "5 min ago", "3 h ago", then the day and time. */
export function relativeTime(date: Date, now: number): string {
  if (Number.isNaN(date.getTime())) return '';
  const seconds = Math.max(0, Math.round((now - date.getTime()) / 1000));
  if (seconds < 45) return 'just now';
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes} min ago`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return `${hours} h ago`;
  return `${dayLabel(date, now)}, ${clockTime(date)}`;
}
