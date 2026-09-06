import { Injectable, signal, effect } from '@angular/core';
import { defaultFeatureFlags, type FeatureFlags } from './feature-flags';

const STORAGE_KEY = 'snap-rq-feature-flags';

/**
 * Runtime feature flags. Defaults are defined in the environment file and can
 * be overridden per-browser via localStorage. The UI exposes a toggle so users
 * can swap between the classic request list and the experimental V2 list.
 */
@Injectable({ providedIn: 'root' })
export class FeatureFlagsService {
  readonly useRequestListV2 = signal<boolean>(
    this.loadFlag('useRequestListV2', defaultFeatureFlags.useRequestListV2),
  );

  constructor() {
    effect(() => {
      try {
        const value = this.useRequestListV2();
        localStorage.setItem(STORAGE_KEY, JSON.stringify({ useRequestListV2: value }));
      } catch {
        // localStorage may be unavailable in some environments.
      }
    });
  }

  toggleRequestListV2(): void {
    this.useRequestListV2.update((value) => !value);
  }

  private loadFlag<K extends keyof FeatureFlags>(
    key: K,
    fallback: FeatureFlags[K],
  ): FeatureFlags[K] {
    try {
      const raw = localStorage.getItem(STORAGE_KEY);
      if (raw) {
        const parsed = JSON.parse(raw) as Partial<FeatureFlags>;
        if (typeof parsed[key] === typeof fallback) {
          return parsed[key] as FeatureFlags[K];
        }
      }
    } catch {
      // Ignore corrupted or unavailable storage.
    }
    return fallback;
  }
}
