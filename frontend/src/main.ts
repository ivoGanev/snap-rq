import { bootstrapApplication } from '@angular/platform-browser';
import { appConfig } from './app/app.config';
import { App } from './app/app';
import { exposeForE2E } from './app/core/testing/e2e-hook';

// Import the generated event bindings so that the typed `time` event
// declared in Go is registered with the runtime.
import '../bindings/github.com/wailsapp/wails/v3/internal/eventcreate';

bootstrapApplication(App, appConfig)
  .then((app) => exposeForE2E(app))
  .catch((err) => console.error(err));
