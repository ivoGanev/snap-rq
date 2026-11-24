import { Component, DestroyRef, computed, inject, input, output, signal } from '@angular/core';
import type { HttpResponse } from '../core/models/http';
import { httpStatusClass, httpStatusText, isRequestError } from '../core/http-status';
import { clockTime, dayLabel, parseSavedTime, relativeTime } from '../core/time';

/** Whether the timeline is open; kept between requests and sessions. */
const OPEN_KEY = 'response-history:open';

/** One stop on the timeline. */
interface Stop {
  response: HttpResponse;
  time: string;
  status: string;
  /** Colours the dot and the status: 2xx / 3xx / 4xx / 5xx / error. */
  kind: string;
  passed: number;
  failed: number;
}

/** Stops sent on the same day, under that day's heading. */
interface Day {
  label: string;
  stops: Stop[];
}

/**
 * A request's response history. Folded, it is one line: the response shown
 * below (clicking anywhere on it unfolds the timeline). Unfolded, a vertical
 * timeline grouped by day, newest first, each stop with its time, status,
 * duration and test results. Only the user folds and unfolds it.
 */
@Component({
  selector: 'app-response-history',
  templateUrl: './response-history.html',
  styleUrl: './response-history.scss',
})
export class ResponseHistory {
  /** Newest first, as the backend lists them. */
  readonly responses = input.required<HttpResponse[]>();
  readonly selected = input<HttpResponse | null>(null);
  readonly select = output<HttpResponse>();
  /** Shows the bin that clears the whole history. */
  readonly clearable = input(false);
  readonly clear = output<void>();

  protected readonly open = signal(readOpen());
  /** Ticks, so "2 min ago" stays true. */
  private readonly now = signal(Date.now());

  /** The folded line's summary of the response shown below. */
  protected readonly current = computed(() => {
    const response = this.selected();
    return response ? this.stop(response) : null;
  });
  protected readonly currentAgo = computed(() => {
    const response = this.selected();
    return response ? relativeTime(parseSavedTime(response.created_at), this.now()) : '';
  });

  protected readonly days = computed<Day[]>(() => {
    const days: Day[] = [];
    for (const response of this.responses()) {
      const label = dayLabel(parseSavedTime(response.created_at), this.now());
      const day = days.at(-1);
      if (day?.label === label) day.stops.push(this.stop(response));
      else days.push({ label, stops: [this.stop(response)] });
    }
    return days;
  });

  constructor() {
    const timer = setInterval(() => this.now.set(Date.now()), 30_000);
    inject(DestroyRef).onDestroy(() => clearInterval(timer));
  }

  protected toggle(): void {
    this.open.update((open) => !open);
    writeOpen(this.open());
  }

  protected isSelected(response: HttpResponse): boolean {
    return response.id === this.selected()?.id;
  }

  private stop(response: HttpResponse): Stop {
    let passed = 0;
    let failed = 0;
    for (const run of response.script_log ?? []) {
      for (const test of run.tests ?? []) {
        if (test.passed) passed++;
        else failed++;
      }
      if (run.error) failed++;
    }
    const error = isRequestError(response.status_code);
    return {
      response,
      time: clockTime(parseSavedTime(response.created_at)),
      status: error
        ? 'Error'
        : `${response.status_code} ${httpStatusText(response.status_code)}`.trim(),
      kind: error ? 'error' : httpStatusClass(response.status_code),
      passed,
      failed,
    };
  }
}

// The open state is a per-viewer convenience: browser storage may be
// unavailable, so failures fall back to folded.
function readOpen(): boolean {
  try {
    return localStorage.getItem(OPEN_KEY) === '1';
  } catch {
    return false;
  }
}

function writeOpen(open: boolean): void {
  try {
    localStorage.setItem(OPEN_KEY, open ? '1' : '0');
  } catch {
    // not remembered; harmless
  }
}
