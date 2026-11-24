import { APP_INITIALIZER, ApplicationConfig, provideBrowserGlobalErrorListeners } from '@angular/core';
import { provideRouter } from '@angular/router';

import { routes } from './app.routes';
import { ThemeService } from './core/services/theme.service';
import { IconManifestService } from './core/services/icon-manifest.service';
import { setVariableNameRule } from './core/models/environment-context';
import { VariablesService } from '../../bindings/snap-rq';

export const appConfig: ApplicationConfig = {
  providers: [
    provideBrowserGlobalErrorListeners(),
    provideRouter(routes),
    {
      provide: APP_INITIALIZER,
      // The app cannot start without a valid icon registry: theme + icon
      // manifest are loaded and validated before bootstrap completes. The
      // variable name rule comes first too, so no {{name}} is drawn without it.
      useFactory: (theme: ThemeService, icons: IconManifestService) => async () => {
        await theme.ready;
        await icons.ready;
        setVariableNameRule(await VariablesService.NameRule());
      },
      deps: [ThemeService, IconManifestService],
      multi: true,
    },
  ],
};
