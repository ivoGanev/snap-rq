import { test } from '@playwright/test';
import { variableDropdownTests } from './support/variable-dropdown';

// macOS shows the app in WebKit; switching browsers is only allowed at a file's top level.
test.use({ browserName: 'webkit' });

variableDropdownTests();
