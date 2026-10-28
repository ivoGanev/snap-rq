import { Injectable } from '@angular/core';
import * as IconService from '../../../../bindings/snap-rq/backend/services';
import type { IconReplacement } from '../../../../bindings/snap-rq/backend/models';

export type { IconReplacement };

/** Angular wrapper around the Wails-generated IconService bindings. */
@Injectable({ providedIn: 'root' })
export class IconApiService {
  /** One-time migration: rewrites stored icon ids using the given replacements. */
  async migrateIconIds(replacements: IconReplacement[]): Promise<void> {
    await IconService.IconService.MigrateIconIds(replacements);
  }
}
