import { test } from '@playwright/test';
import { requestGroupsTests } from './support/request-groups';

// macOS shows the app in WebKit; switching browsers is only allowed at a file's top level.
test.use({ browserName: 'webkit' });

requestGroupsTests();
