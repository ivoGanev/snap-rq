import { test } from '@playwright/test';
import { variableChipTests } from './support/variable-chips';

// macOS shows the app in WebKit; switching browsers is only allowed at a file's top level.
test.use({ browserName: 'webkit' });

variableChipTests();
