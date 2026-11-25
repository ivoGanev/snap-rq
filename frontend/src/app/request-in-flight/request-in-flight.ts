import { Component, DestroyRef, computed, inject, input, linkedSignal, signal } from '@angular/core';
import { RequestApiService } from '../core/services/request.service';

/** The clock runs here, per frame: the backend's progress ticks come about once a second. */
@Component({
  selector: 'app-request-in-flight',
  templateUrl: './request-in-flight.html',
  styleUrl: './request-in-flight.scss',
})
export class RequestInFlight {
  private readonly requestApi = inject(RequestApiService);

  readonly requestId = input.required<number>();

  private readonly now = signal(Date.now());
  // Kept after the send ends, while the view is still held on screen.
  private readonly startedAt = linkedSignal<number | undefined, number | undefined>({
    source: () => this.requestApi.sending()[this.requestId()],
    computation: (startedAt, previous) => startedAt ?? previous?.value,
  });

  protected readonly elapsed = computed(() => {
    const startedAt = this.startedAt();
    return startedAt === undefined ? '' : formatElapsed(this.now() - startedAt);
  });

  constructor() {
    let frame = 0;
    const tick = () => {
      if (this.requestApi.sending()[this.requestId()] !== undefined) this.now.set(Date.now());
      frame = requestAnimationFrame(tick);
    };
    frame = requestAnimationFrame(tick);
    inject(DestroyRef).onDestroy(() => cancelAnimationFrame(frame));
  }

  protected cancel(): void {
    this.requestApi.cancel(this.requestId());
  }
}

function formatElapsed(ms: number): string {
  if (ms < 1000) return `${Math.max(0, Math.round(ms))} ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(2)} s`;
  const minutes = Math.floor(ms / 60_000);
  const seconds = ((ms % 60_000) / 1000).toFixed(1).padStart(4, '0');
  return `${minutes}:${seconds}`;
}
