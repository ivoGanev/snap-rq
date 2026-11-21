import { resolve } from 'node:path';

// Shared by playwright.config.ts and global-setup.ts. Ports can be moved
// with E2E_APP_PORT / E2E_STUB_PORT (e.g. on a shared CI machine).

export const APP_PORT = Number(process.env['E2E_APP_PORT'] ?? 9871);
export const STUB_PORT = Number(process.env['E2E_STUB_PORT'] ?? 9872);

export const APP_URL = `http://localhost:${APP_PORT}`;
export const STUB_URL = `http://127.0.0.1:${STUB_PORT}`;

/** The server-mode binary `task test:e2e` builds (e2e frontend embedded). */
export const APP_BINARY = resolve(__dirname, '..', '..', '..', 'bin', `snaprq-e2e${process.platform === 'win32' ? '.exe' : ''}`);
