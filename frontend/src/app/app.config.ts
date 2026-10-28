import { APP_INITIALIZER, ApplicationConfig, provideBrowserGlobalErrorListeners } from '@angular/core';
import { provideRouter } from '@angular/router';

import { routes } from './app.routes';
import { ThemeService } from './core/services/theme.service';
import { IconManifestService } from './core/services/icon-manifest.service';

export const appConfig: ApplicationConfig = {
  providers: [
    provideBrowserGlobalErrorListeners(),
    provideRouter(routes),
    {
      provide: APP_INITIALIZER,
      // The app cannot start without a valid icon registry: theme + icon
      // manifest are loaded and validated before bootstrap completes.
      useFactory: (theme: ThemeService, icons: IconManifestService) => async () => {
        await theme.ready;
        await icons.ready;
      },
      deps: [ThemeService, IconManifestService],
      multi: true,
    },
  ],
};
