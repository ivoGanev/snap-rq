import { test } from '@playwright/test';
import { listToolbarTests } from './support/list-toolbar';

// macOS shows the app in WebKit; switching browsers is only allowed at a file's top level.
test.use({ browserName: 'webkit' });

listToolbarTests();
