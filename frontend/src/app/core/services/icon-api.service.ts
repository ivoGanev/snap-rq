import { Injectable } from '@angular/core';
import * as IconService from '../../../../bindings/snap-rq/backend/services';

/** Angular wrapper around the Wails-generated IconService bindings. */
@Injectable({ providedIn: 'root' })
export class IconApiService {
  /**
   * Resets any stored icon appearance whose id is not in the current registry
   * to the default icon. Keeps the database in sync when an id is removed.
   */
  async cleanupIconReferences(validIds: string[], defaultId: string): Promise<void> {
    await IconService.IconService.CleanupIconReferences(validIds, defaultId);
  }
}
