import { test } from '@playwright/test';
import { lastUsedPawTests } from './support/last-used-paw';

// macOS shows the app in WebKit; switching browsers is only allowed at a file's top level.
test.use({ browserName: 'webkit' });

lastUsedPawTests();
