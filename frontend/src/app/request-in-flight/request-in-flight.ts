import { Component, DestroyRef, computed, inject, input, signal } from '@angular/core';
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
  readonly method = input('');
  readonly url = input('');

  private readonly now = signal(Date.now());

  protected readonly elapsed = computed(() => {
    const startedAt = this.requestApi.sending()[this.requestId()];
    return startedAt === undefined ? '' : formatElapsed(this.now() - startedAt);
  });

  constructor() {
    let frame = 0;
    const tick = () => {
      this.now.set(Date.now());
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
