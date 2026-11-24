import { expect, test, type Page } from '@playwright/test';
import { createRequest, openApp, STUB } from './app';

const HIDDEN = '•••••';
const SECRETS = ['hunter2', 'tok-access-1', 'tok-header-4', 'sess-5'];

/** A request to `path` with one saved response, opened in the inspector. */
async function openResponse(page: Page, path: string): Promise<void> {
  const id = await createRequest(page, `${STUB}${path}`);
  await page.evaluate(async (id) => {
    const { requests } = window.snaprq;
    await requests.send(await requests.getFull(id), [], null);
  }, id);
  await page.reload();
  await page.waitForFunction(() => window.snaprq !== undefined);
  await page
    .locator('.spreadsheet-row', { hasText: path })
    .first()
    .locator('.spreadsheet-cell--url')
    .click();
}

const pane = (page: Page) => page.locator('app-response-pane');
const action = (page: Page, label: string) =>
  pane(page).locator(`.section-action[aria-label="${label}"]`);
const shown = async (page: Page) =>
  (await pane(page).locator('.response-headers').innerText()) +
  (await pane(page).locator('.response-body').innerText()) +
  (await pane(page).locator('.response-cookies').innerText());

/** Clicks a copy button and returns the text the page handed to the clipboard. */
async function copied(page: Page, label: string): Promise<string> {
  const call = page.waitForRequest(
    (r) => r.url().endsWith('/wails/runtime') && (r.postData() ?? '').startsWith('{"object":1,'),
  );
  await action(page, label).click();
  return JSON.parse((await call).postData()!).args.text as string;
}

export function responseSecretsTests(): void {
  test('a response hides its secrets until revealed, then hides them again for the next one', async ({
    page,
  }) => {
    await openApp(page);
    await openResponse(page, '/secrets');

    await expect(pane(page).locator('.response-body')).toContainText(HIDDEN);
    const hidden = await shown(page);
    for (const secret of SECRETS) expect(hidden).not.toContain(secret);
    expect(hidden).toContain('demo-user');
    expect(hidden).toContain('req-0001');

    await action(page, 'Reveal secrets in the body').click();
    await expect(pane(page).locator('.response-body')).toContainText('hunter2');
    await expect(pane(page).locator('.response-headers')).not.toContainText('tok-header-4');
    await action(page, 'Reveal secrets in the headers').click();
    await expect(pane(page).locator('.response-headers')).toContainText('tok-header-4');
    await expect(pane(page).locator('.response-cookies')).toContainText('sess-5');

    await openResponse(page, '/secrets');
    await expect(pane(page).locator('.response-body')).toContainText(HIDDEN);
    await expect(pane(page).locator('.response-body')).not.toContainText('hunter2');
  });

  test('a response with nothing secret has no reveal button', async ({ page }) => {
    await openApp(page);
    await openResponse(page, '/ok');
    await expect(pane(page).locator('.response-body')).toContainText('"status"');
    await expect(action(page, 'Copy the body')).toBeVisible();
    await expect(pane(page).locator('.section-action[aria-label^="Reveal"]')).toHaveCount(0);
  });

  test('copy takes what is shown: hidden until revealed', async ({ page }) => {
    await openApp(page);
    await openResponse(page, '/secrets');
    await expect(pane(page).locator('.response-body')).toContainText(HIDDEN);

    const body = await copied(page, 'Copy the body');
    expect(body).toContain(`"password": "${HIDDEN}"`);
    expect(body).not.toContain('hunter2');
    const headers = await copied(page, 'Copy the headers');
    expect(headers).toContain('X-Request-Id: req-0001');
    expect(headers).not.toContain('tok-header-4');

    await action(page, 'Reveal secrets in the body').click();
    expect(await copied(page, 'Copy the body')).toContain('"password": "hunter2"');
  });
}
