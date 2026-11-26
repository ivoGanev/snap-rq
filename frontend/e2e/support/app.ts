import { expect, type Page } from '@playwright/test';
import type { SnapRqE2E } from '../../src/app/core/testing/e2e-hook.e2e';

// Helpers that reach the app's real services (window.snaprq, e2e build only).
// Everything runs inside the page, through the real Wails transport to the
// real backend.

declare global {
  interface Window {
    snaprq: SnapRqE2E;
  }
}

import { STUB_URL } from './env';

/** The stub server's base URL (see playwright.config.ts). */
export { STUB_URL as STUB } from './env';

/** Opens the app and waits until its services are reachable. */
export async function openApp(page: Page): Promise<void> {
  await page.goto('/');
  await page.waitForFunction(() => window.snaprq !== undefined);
}

/** Creates a GET request to `url` in the first collection; returns its id. */
export async function createRequest(page: Page, url: string, headers = ''): Promise<number> {
  return page.evaluate(async ({ url, headers }) => {
    const { commands, requests } = window.snaprq;
    const [project] = await commands.execute<{ id: number }[]>('project.list', {});
    const [collection] = await commands.execute<{ id: number; project_id: number }[]>('collection.list', {
      project_id: project.id,
    });
    const created = await requests.create({
      collection_id: collection.id,
      project_id: collection.project_id,
      name: `e2e ${url}`,
      url,
      method: 'GET',
      body: '',
      body_type: 'json',
      body_form: '',
      request_headers: headers,
      status_code: 0,
      response_id: 0,
    });
    return created.id;
  }, { url, headers });
}

/**
 * Sends a new request to the stub's `endpoint` once, then opens its response
 * in the inspector. `query` is added to the URL as sent.
 */
export async function openResponse(page: Page, endpoint: string, query = ''): Promise<void> {
  // Other tests' rows share the endpoint, so the row is found by a unique query.
  const path = `${endpoint}?run=${Date.now()}${query ? `&${query}` : ''}`;
  const id = await createRequest(page, `${STUB_URL}${path}`);
  await page.evaluate(async (id) => {
    const { requests } = window.snaprq;
    await requests.send(await requests.getFull(id));
  }, id);
  await page.reload();
  await page.waitForFunction(() => window.snaprq !== undefined);
  await page
    .locator('.spreadsheet-row', { hasText: path })
    .first()
    .locator('.spreadsheet-cell--url')
    .click();
}

/** Opens the request in the workspace, as clicking its row does. */
export async function openRequest(page: Page, id: number): Promise<void> {
  await page.evaluate(async (id) => {
    const { requests, workspace } = window.snaprq;
    workspace.selectedRequest.set(await requests.getFull(id));
  }, id);
}

/** Whether the frontend still shows the request as in flight. */
export function inFlight(page: Page, id: number): Promise<boolean> {
  return page.evaluate((id) => window.snaprq.requests.sending()[id] !== undefined, id);
}

/** Waits until the request is no longer in flight (fails after `timeout`). */
export async function expectSettled(page: Page, id: number, timeout = 5_000): Promise<void> {
  await expect.poll(() => inFlight(page, id), { timeout, message: `request ${id} is stuck in flight` }).toBe(false);
}
