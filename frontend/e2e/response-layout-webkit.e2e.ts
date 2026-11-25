import { test } from '@playwright/test';
import { responseLayoutTests } from './support/response-layout';

// macOS shows the app in WebKit; switching browsers is only allowed at a file's top level.
test.use({ browserName: 'webkit' });

responseLayoutTests();
