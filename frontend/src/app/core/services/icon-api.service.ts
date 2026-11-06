import { Injectable, inject } from '@angular/core';
import { CommandApiService } from './command.service';

/**
 * The app's typed API for icon maintenance.
 */
@Injectable({ providedIn: 'root' })
export class IconApiService {
  private readonly commands = inject(CommandApiService);

  async cleanupIconReferences(validIds: string[], defaultId: string): Promise<void> {
    await this.commands.execute('icon.cleanup', { valid_ids: validIds, default_id: defaultId });
  }
}