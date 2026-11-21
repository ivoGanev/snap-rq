import type { ApplicationRef } from '@angular/core';

/**
 * Exposes the app's real services to the end-to-end tests. In every normal
 * build this does nothing; the "e2e" build configuration (angular.json)
 * swaps in e2e-hook.e2e.ts.
 */
export function exposeForE2E(_app: ApplicationRef): void {}
