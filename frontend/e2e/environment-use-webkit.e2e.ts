import { test } from '@playwright/test';
import { environmentUseTests } from './support/environment-use';

// macOS shows the app in WebKit; switching browsers is only allowed at a file's top level.
test.use({ browserName: 'webkit' });

environmentUseTests();
