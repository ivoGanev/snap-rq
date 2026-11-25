import { expect, test } from '@playwright/test';
import { createRequest, openApp, openRequest, STUB } from './support/app';

// Regression: a request that failed before any response (here a malformed
// header) was shown as a response with status 0: a green check, "0",
// "No headers", the message squeezed into Body.
test('a failed request shows its error, not status 0', async ({ page }) => {
  await openApp(page);
  // A JSON object typed as headers: no header can be named '{"Accept"'.
  const id = await createRequest(page, `${STUB}/ok`, '{"Accept": "application/json"}');
  await openRequest(page, id);

  await page.evaluate(async (id) => {
    const { requests } = window.snaprq;
    await requests.send(await requests.getFull(id), null);
  }, id);

  const inspector = page.getByLabel('Inspector');
  const error = inspector.getByRole('alert');
  await expect(error).toContainText("Couldn't send the request");
  await expect(error).toContainText(`Header line 1 isn't a header: {"Accept": "application/json"}`);
  await expect(error).toContainText('Name: value');
  await expect(error).not.toContainText('request failed:');
  await expect(error.locator('.request-error-details')).toHaveCount(0);
  await expect(inspector.locator('.history-current-status')).toHaveText('Error');

  // None of the response layout.
  await expect(inspector.locator('.status-line')).toHaveCount(0);
  await expect(inspector.getByText('No headers')).toHaveCount(0);
});

test('a failed send explains what went wrong and keeps the raw error under Details', async ({ page }) => {
  await openApp(page);
  // Nothing listens on port 1.
  const id = await createRequest(page, 'http://127.0.0.1:1/');
  await openRequest(page, id);

  await page.evaluate(async (id) => {
    const { requests } = window.snaprq;
    await requests.send(await requests.getFull(id), null);
  }, id);

  const error = page.getByLabel('Inspector').getByRole('alert');
  await expect(error.locator('.request-error-message')).toHaveText('Nothing is answering at 127.0.0.1:1. Is the server running?');
  const details = error.locator('.request-error-details');
  await expect(details.locator('pre')).toBeHidden();
  await details.getByText('Details').click();
  await expect(details.locator('pre')).toBeVisible();
  await expect(details.locator('pre')).toContainText('request failed:');
});
