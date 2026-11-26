import { test } from '@playwright/test';
import { updateIconTests } from './support/update-icon';

// macOS shows the app in WebKit; switching browsers is only allowed at a file's top level.
test.use({ browserName: 'webkit' });

updateIconTests();
