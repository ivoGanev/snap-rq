import { expect, test, type Page } from '@playwright/test';
import { openApp, STUB } from './support/app';

interface Binned {
  collectionId: number;
  collectionName: string;
}

/** Creates a collection with the named requests and moves them all to the bin. */
async function binRequests(page: Page, names: string[]): Promise<Binned> {
  const collectionName = `bin ${Date.now()} ${Math.random().toString(36).slice(2, 6)}`;
  const collectionId = await page.evaluate(
    async ({ url, names, collectionName }) => {
      const { commands, requests } = window.snaprq;
      const [project] = await commands.execute<{ id: number }[]>('project.list', {});
      const collection = await commands.execute<{ id: number }>('collection.create', {
        project_id: project.id,
        name: collectionName,
      });
      const ids: number[] = [];
      for (const name of names) {
        const req = await requests.create({
          collection_id: collection.id,
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
        });
        ids.push(req.id);
      }
      await commands.execute('request.delete', { ids });
      return collection.id;
    },
    { url: `${STUB}/ok`, names, collectionName },
  );
  return { collectionId, collectionName };
}

async function openBin(page: Page): Promise<void> {
  await page.reload();
  await page.waitForFunction(() => window.snaprq !== undefined);
  await page.getByRole('button', { name: 'Trash bin', exact: true }).click();
}

const row = (page: Page, name: string) => page.locator('.spreadsheet-row', { hasText: name });

function select(page: Page, name: string, add = false): Promise<void> {
  return row(page, name)
    .locator('.spreadsheet-cell--url')
    .click(add ? { modifiers: ['ControlOrMeta'] } : undefined);
}

const confirmDialog = (page: Page) => page.locator('.confirm-popup');

test('deleting permanently from the inspector asks first', async ({ page }) => {
  await openApp(page);
  const tag = Date.now();
  const [one, two] = [`keep one ${tag}`, `keep two ${tag}`];
  await binRequests(page, [one, two]);
  await openBin(page);

  await select(page, one);
  await page
    .locator('app-binned-request-pane')
    .getByRole('button', { name: 'Delete permanently' })
    .click();
  await expect(confirmDialog(page)).toBeVisible();
  await confirmDialog(page).getByRole('button', { name: 'Cancel' }).click();
  await expect(row(page, one)).toBeVisible();

  await select(page, two, true);
  await page
    .locator('app-binned-requests-pane')
    .getByRole('button', { name: 'Delete permanently' })
    .click();
  await expect(confirmDialog(page)).toContainText('These 2 requests');
  await confirmDialog(page).getByRole('button', { name: 'Cancel' }).click();
  await expect(row(page, one)).toBeVisible();
  await expect(row(page, two)).toBeVisible();

  await page
    .locator('app-binned-requests-pane')
    .getByRole('button', { name: 'Delete permanently' })
    .click();
  await confirmDialog(page).getByRole('button', { name: 'Delete permanently' }).click();
  await expect(row(page, one)).toHaveCount(0);
  await expect(row(page, two)).toHaveCount(0);
});

test('empty bin deletes everything in the bin, only once confirmed', async ({ page }) => {
  await openApp(page);
  const tag = Date.now();
  await binRequests(page, [`a ${tag}`, `b ${tag}`, `c ${tag}`]);
  await openBin(page);

  const emptyBin = page.locator('.top-bar-empty-bin');
  await emptyBin.click();
  await confirmDialog(page).getByRole('button', { name: 'Cancel' }).click();
  await expect(row(page, `a ${tag}`)).toBeVisible();
  await expect(row(page, `c ${tag}`)).toBeVisible();

  await emptyBin.click();
  await expect(confirmDialog(page)).toContainText('permanently deleted');
  await confirmDialog(page).getByRole('button', { name: 'Empty bin' }).click();
  await expect(page.locator('.spreadsheet-row')).toHaveCount(0);
  await expect(page.locator('.state-message--bin')).toBeVisible();
  await expect(emptyBin).toHaveCount(0);
  const left = await page.evaluate(async () => {
    const { commands } = window.snaprq;
    const [project] = await commands.execute<{ id: number }[]>('project.list', {});
    return (await commands.execute<unknown[] | null>('bin.list', { project_id: project.id })) ?? [];
  });
  expect(left).toEqual([]);
});

test('restore goes back to the original collection, or asks where when it is gone', async ({
  page,
}) => {
  await openApp(page);
  const tag = Date.now();
  const home = `home ${tag}`;
  const { collectionId, collectionName } = await binRequests(page, [home]);
  const orphanA = `orphan a ${tag}`;
  const orphanB = `orphan b ${tag}`;
  const gone = await binRequests(page, [orphanA]);
  await binRequests(page, [orphanB]);
  await page.evaluate(
    (id) => window.snaprq.commands.execute('collection.delete', { id }),
    gone.collectionId,
  );
  await openBin(page);

  await select(page, home);
  const restore = page
    .locator('app-binned-request-pane')
    .getByRole('button', { name: `Restore to ${collectionName}` });
  await restore.click();
  await expect(row(page, home)).toHaveCount(0);
  const restored = await page.evaluate(
    (id) =>
      window.snaprq.commands.execute<{ name: string }[]>('request.list_for_collection', {
        collection_id: id,
      }),
    collectionId,
  );
  expect(restored.map((r) => r.name)).toContain(home);

  await select(page, orphanA);
  await page.locator('app-binned-request-pane').getByRole('button', { name: 'Restore…' }).click();
  await expect(page.locator('.restore-to-collection-dialog')).toBeVisible();
  await page
    .locator('.restore-to-collection-dialog')
    .getByRole('button', { name: 'Cancel' })
    .click();

  // One of the two has lost its collection, so nothing is restored until a collection is picked.
  await select(page, orphanB, true);
  await page
    .locator('app-binned-requests-pane')
    .getByRole('button', { name: 'Restore all' })
    .click();
  await expect(page.locator('.restore-to-collection-dialog')).toBeVisible();
  await page
    .locator('.restore-to-collection-dialog')
    .getByRole('button', { name: 'Cancel' })
    .click();
  await expect(row(page, orphanA)).toBeVisible();
  await expect(row(page, orphanB)).toBeVisible();
});
