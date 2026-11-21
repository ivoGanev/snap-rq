import { expect, test } from '@playwright/test';
import { createRequest, openApp, openRequest, STUB } from './support/app';

// Regression: a request that failed before any response (here a malformed
// header) was shown as a response with status 0: a green check, "0",
// "No headers", the message squeezed into Body.
test('a failed request shows its error, not status 0', async ({ page }) => {
  await openApp(page);
  // A JSON object typed as headers: Go rejects the header name '{"Accept"'.
  const id = await createRequest(page, `${STUB}/ok`, '{"Accept": "application/json"}');
  await openRequest(page, id);

  await page.evaluate(async (id) => {
    const { requests } = window.snaprq;
    await requests.send(await requests.getFull(id), [], null);
  }, id);

  const inspector = page.getByLabel('Inspector');
  const error = inspector.getByRole('alert');
  await expect(error).toContainText("Couldn't send the request");
  await expect(error).toContainText('invalid header field name');
  await expect(error).not.toContainText('request failed:');
  await expect(inspector.locator('.response-chip')).toContainText('Error');

  // None of the response layout.
  await expect(inspector.locator('.status-badge')).toHaveCount(0);
  await expect(inspector.getByText('No headers')).toHaveCount(0);
});
