import { test } from '@playwright/test';
import { sidebarTests } from './support/sidebar';

// macOS shows the app in WebKit; switching browsers is only allowed at a file's top level.
test.use({ browserName: 'webkit' });

sidebarTests();
