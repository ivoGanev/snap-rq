import type { ApplicationRef } from '@angular/core';
import { CommandApiService } from '../services/command.service';
import { RequestApiService } from '../services/request.service';

/** The app's real services, for the end-to-end tests (see frontend/e2e). */
export interface SnapRqE2E {
  commands: CommandApiService;
  requests: RequestApiService;
}

/** e2e build only: puts the real services on window.snaprq. */
export function exposeForE2E(app: ApplicationRef): void {
  const handle: SnapRqE2E = {
    commands: app.injector.get(CommandApiService),
    requests: app.injector.get(RequestApiService),
  };
  (window as unknown as { snaprq: SnapRqE2E }).snaprq = handle;
}
