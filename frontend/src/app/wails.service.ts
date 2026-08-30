import { Injectable, OnDestroy, signal } from '@angular/core';
import { Events } from '@wailsio/runtime';

@Injectable({ providedIn: 'root' })
export class WailsService implements OnDestroy {
  readonly currentTime = signal<string>('Listening for Time event...');
  private readonly offTime: () => void;

  constructor() {
    this.offTime = Events.On('time', (event) => {
      const full = event.data;
      const compact = (full.match(/\d{1,2}:\d{2}:\d{2}/) || [full])[0];
      this.currentTime.set(
        window.matchMedia('(max-width: 640px)').matches ? compact : full,
      );
    });
  }

  ngOnDestroy(): void {
    this.offTime();
  }
}
