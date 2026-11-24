import { test } from '@playwright/test';
import { responseSecretsTests } from './support/response-secrets';

// macOS shows the app in WebKit; switching browsers is only allowed at a file's top level.
test.use({ browserName: 'webkit' });

responseSecretsTests();
