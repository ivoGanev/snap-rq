import { test } from '@playwright/test';
import { topBarTests } from './support/top-bar';

// macOS shows the app in WebKit; switching browsers is only allowed at a file's top level.
test.use({ browserName: 'webkit' });

topBarTests();
