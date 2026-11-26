import { expect, test, type Page } from '@playwright/test';
import { openApp, STUB } from './app';

function send(page: Page, id: number): Promise<void> {
  return page.evaluate(async (id) => {
    const { requests } = window.snaprq;
    await requests.send(await requests.getFull(id));
  }, id);
}

export function lastUsedPawTests(): void {
  test('the paw marks the last sent request and its collection', async ({ page }) => {
    await openApp(page);
    const collection = `paw ${Date.now()}`;
    const ids = await page.evaluate(
      async ({ url, collection }) => {
        const { commands, requests } = window.snaprq;
        const [project] = await commands.execute<{ id: number }[]>('project.list', {});
        const [elsewhere] = await commands.execute<{ id: number }[]>('collection.list', {
          project_id: project.id,
        });
        const created = await commands.execute<{ id: number }>('collection.create', {
          project_id: project.id,
          name: collection,
        });
        const make = async (collectionId: number, name: string) =>
          (
            await requests.create({
              collection_id: collectionId,
              project_id: project.id,
              name,
              url,
              method: 'GET',
              body: '',
              body_type: 'json',
              body_form: '',
              request_headers: '',
              status_code: 0,
              response_id: 0,
            })
          ).id;
        return {
          first: await make(created.id, 'first sent'),
          last: await make(created.id, 'sent last'),
          never: await make(created.id, 'never sent'),
          elsewhere: await make(elsewhere.id, `elsewhere ${collection}`),
        };
      },
      { url: `${STUB}/ok`, collection },
    );
    await send(page, ids.first);
    await send(page, ids.last);
    await page.reload();
    await page.waitForFunction(() => window.snaprq !== undefined);

    await page.getByRole('button', { name: 'Collections', exact: true }).click();
    const item = page.locator(
      `.group-list-item[data-item-kind="collection"][data-item-name="${collection}"]`,
    );
    const paws = page.locator('.group-list-item .last-used-paw');
    await expect(item.locator('.last-used-paw')).toBeVisible();
    await expect(paws).toHaveCount(1);

    // The test window's sidebar is too narrow to show the name, so the item is clicked directly.
    await item.dispatchEvent('click');
    const row = (name: string) => page.locator('.spreadsheet-row', { hasText: name });
    await expect(row('sent last').locator('.last-used-paw')).toBeVisible();
    await expect(page.locator('.spreadsheet-row .last-used-paw')).toHaveCount(1);
    await expect(row('sent last').locator('.last-used-paw svg')).toHaveCSS('width', '14px');

    // Both paws follow a new send without a reload.
    await send(page, ids.never);
    await expect(row('never sent').locator('.last-used-paw')).toBeVisible();
    await expect(page.locator('.spreadsheet-row .last-used-paw')).toHaveCount(1);

    await send(page, ids.elsewhere);
    await expect(item.locator('.last-used-paw')).toHaveCount(0);
    await expect(paws).toHaveCount(1);
  });
}
