import { defineConfig } from '@playwright/test';
import { APP_URL, STUB_PORT, STUB_URL } from './e2e/support/env';

/*
 * End-to-end tests: the real frontend services drive the real Go backend
 * (Wails server mode: calls over HTTP, events over a WebSocket) and a real
 * SQLite database in a throwaway folder. Nothing is mocked; the only extra
 * piece is a local HTTP server the requests call (e2e/support/stub-server.mjs).
 *
 * Run everything with `task test:e2e` from the repository root.
 */
export default defineConfig({
  testDir: './e2e',
  testMatch: '**/*.e2e.ts',
  // Starts the app on a fresh data folder; its teardown deletes the folder.
  globalSetup: './e2e/support/global-setup.ts',
  // The tests share one app; each creates its own data.
  workers: 1,
  timeout: 30_000,
  reporter: process.env['CI'] ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL: APP_URL,
    trace: 'retain-on-failure',
  },
  webServer: {
    command: `node e2e/support/stub-server.mjs ${STUB_PORT}`,
    url: `${STUB_URL}/ok`,
    reuseExistingServer: false,
  },
});
