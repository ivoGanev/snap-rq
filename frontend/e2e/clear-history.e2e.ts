import { expect, test, type Page } from '@playwright/test';
import { createRequest, openApp, STUB } from './support/app';

/** A request sent twice to a failing endpoint, so it has history and shows under Failing. */
async function failingRequest(page: Page, name: string): Promise<number> {
  const id = await createRequest(page, `${STUB}/status/500`);
  await page.evaluate(
    async ({ id, name }) => {
      const { commands, requests } = window.snaprq;
      await commands.execute('request.bulk_set', { ids: [id], field: 'name', value: name });
      for (let i = 0; i < 2; i++) await requests.send(await requests.getFull(id));
    },
    { id, name },
  );
  await page.reload();
  await page.waitForFunction(() => window.snaprq !== undefined);
  return id;
}

const row = (page: Page, name: string) => page.locator('.spreadsheet-row', { hasText: name });
const pane = (page: Page) => page.locator('app-response-pane');
const confirmDialog = (page: Page) => page.locator('.confirm-popup');

test('clearing the history asks first, then empties it and the request status', async ({
  page,
}) => {
  await openApp(page);
  const name = `clear me ${Date.now()}`;
  const id = await failingRequest(page, name);

  await page.locator('.sidebar-toolbar').getByRole('button', { name: 'Smart views', exact: true }).click();
  await page.getByRole('button', { name: /^Failing/ }).click();
  await row(page, name).locator('.spreadsheet-cell--url').click();
  await expect(pane(page).locator('.history-bar')).toBeVisible();

  await pane(page).locator('.history-clear').click();
  await expect(confirmDialog(page)).toContainText('All 2 saved responses');
  await confirmDialog(page).getByRole('button', { name: 'Cancel' }).click();
  await expect(pane(page).locator('.history-bar')).toBeVisible();

  await pane(page).locator('.history-clear').click();
  await confirmDialog(page).getByRole('button', { name: 'Clear history' }).click();
  await expect(pane(page).getByText('No responses for this request.')).toBeVisible();
  await expect(pane(page).locator('.history-bar')).toHaveCount(0);
  await expect(row(page, name)).toHaveCount(0);

  const stored = await page.evaluate(async (id) => {
    const { commands } = window.snaprq;
    const req = await commands.execute<{ status_code: number; response_id: number }>(
      'request.get',
      { id },
    );
    const responses = await commands.execute<unknown[] | null>('response.list_for_request', {
      request_id: id,
    });
    return {
      status: req.status_code,
      responseId: req.response_id,
      responses: (responses ?? []).length,
    };
  }, id);
  expect(stored).toEqual({ status: 0, responseId: 0, responses: 0 });
});

test('the bin is in the inspector only, and clicking it leaves the timeline as it was', async ({
  page,
}) => {
  await openApp(page);
  const name = `bin only ${Date.now()}`;
  await failingRequest(page, name);
  await row(page, name).locator('.spreadsheet-cell--url').click();

  const timeline = pane(page).locator('.history-timeline');
  const wasOpen = (await timeline.count()) > 0;
  await pane(page).locator('.history-clear').click();
  await confirmDialog(page).getByRole('button', { name: 'Cancel' }).click();
  await expect(timeline).toHaveCount(wasOpen ? 1 : 0);

  await row(page, name).locator('.spreadsheet-cell--url').dblclick();
  await expect(page.locator('app-zen-mode .history-bar')).toBeVisible();
  await expect(page.locator('app-zen-mode .history-clear')).toHaveCount(0);
});
