import { expect, test, type Page } from '@playwright/test';
import { openApp } from './app';
import { heading, layout, reopen, row, seedCollection, START } from './request-groups';

// The request list's toolbar does the editing: one request's method, name
// and URL; a picked group's name, colour and Ungroup; a selection's actions.
// Rows hold no edit boxes; a double-click opens zen mode.

const url = (page: Page, name: string) => row(page, name).locator('.spreadsheet-cell--url');

async function selectRows(page: Page, ...names: string[]): Promise<void> {
  await url(page, names[0]).click();
  for (const name of names.slice(1)) await url(page, name).click({ modifiers: ['ControlOrMeta'] });
}

const tool = (page: Page, title: string) => page.locator(`.selection-tools .selection-tool[title^="${title}"]`);

export function listToolbarTests(): void {
  test.use({ viewport: { width: 1400, height: 900 } });

  test('in a collection, the selection toolbar groups the selected requests', async ({ page }) => {
    await openApp(page);
    const name = await seedCollection(page);
    await reopen(page, name);

    await selectRows(page, 'alpha', 'delta');
    await expect(page.locator('.selection-count')).toHaveText('2 selected');
    for (const title of ['Group', 'Move to collection', 'Scripts', 'Add to favourites', 'Duplicate', 'Move to Trash']) {
      await expect(tool(page, title)).toBeVisible();
    }
    await expect(tool(page, 'Remove from group')).toHaveCount(0);

    await tool(page, 'Group').click();
    await expect(page.locator('.group-tools-name')).toBeFocused();
    await expect(page.locator('.group-tools-name')).toHaveValue('New group');
    await page.keyboard.type('Pair');
    await page.keyboard.press('Enter');
    await expect.poll(() => layout(page)).toContain('# Pair');
    const rows = await layout(page);
    expect(rows.slice(rows.indexOf('# Pair'), rows.indexOf('# Pair') + 3)).toEqual(['# Pair', '  alpha', '  delta']);
  });

  test('Ungroup in the selection toolbar takes the requests out, and the emptied group goes', async ({ page }) => {
    await openApp(page);
    const name = await seedCollection(page);
    await reopen(page, name);

    await selectRows(page, 'bravo', 'charlie');
    await tool(page, 'Remove from group').click();
    await expect.poll(() => layout(page)).not.toContain('# Auth');
    expect(await layout(page)).toContain('bravo');
  });

  test('Duplicate and Trash in the selection toolbar act on every selected request', async ({ page }) => {
    await openApp(page);
    const name = await seedCollection(page);
    await reopen(page, name);

    await selectRows(page, 'golf', 'hotel');
    await tool(page, 'Duplicate').click();
    await expect.poll(() => layout(page)).toEqual(expect.arrayContaining(['golf (copy)', 'hotel (copy)']));

    await selectRows(page, 'golf (copy)', 'hotel (copy)');
    await tool(page, 'Move to Trash').click();
    await expect.poll(() => layout(page)).toEqual(START);
  });

  test('Move, Scripts and Favourite in the selection toolbar open their pickers', async ({ page }) => {
    await openApp(page);
    const name = await seedCollection(page);
    await reopen(page, name);
    await selectRows(page, 'alpha', 'delta');

    await tool(page, 'Move to collection').click();
    await expect(page.locator('.move-collection-modal')).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(page.locator('.move-collection-modal')).toHaveCount(0);

    await tool(page, 'Scripts').click();
    await expect(page.locator('.script-picker-dialog')).toBeVisible();
    await page.locator('.script-picker-dialog .overlay-close').click();

    await tool(page, 'Add to favourites').click();
    await expect(page.locator('.favourite-picker-dialog')).toBeVisible();
  });

  test('outside collections the selection toolbar has no group buttons', async ({ page }) => {
    await openApp(page);
    const tag = `${Date.now()}`;
    await page.evaluate(async (tag) => {
      const { commands } = window.snaprq;
      const [project] = await commands.execute<{ id: number }[]>('project.list', {});
      const [collection] = await commands.execute<{ id: number }[]>('collection.list', { project_id: project.id });
      await commands.execute('request.create_many', {
        requests: ['one', 'two'].map((n) => ({ collection_id: collection.id, name: `0 ${n} ${tag}`, url: 'https://example.com', method: 'GET' })),
      });
    }, tag);
    await page.reload();
    await page.waitForFunction(() => window.snaprq?.workspace.view() != null);
    await page.locator('.sidebar-toolbar').getByRole('button', { name: 'Smart views', exact: true }).click();
    await page.locator('.smart-view-item', { hasText: 'All requests' }).click();

    // Named to sort first: the list only draws the rows in view.
    await selectRows(page, `0 one ${tag}`, `0 two ${tag}`);
    await expect(tool(page, 'Duplicate')).toBeVisible();
    await expect(tool(page, 'Group')).toHaveCount(0);
    await expect(tool(page, 'Remove from group')).toHaveCount(0);
  });

  test('in a narrow list the selection tools show icons only, and all of them fit', async ({ page }) => {
    await page.setViewportSize({ width: 1000, height: 800 });
    await openApp(page);
    const name = await seedCollection(page);
    await reopen(page, name);

    await selectRows(page, 'alpha', 'bravo');
    await expect(page.locator('.selection-tool-label').first()).toBeHidden();
    const fits = await page.locator('.selection-tools').evaluate((el) => el.scrollWidth <= el.clientWidth);
    expect(fits).toBe(true);
  });

  test('the row menu offers Group and Remove from group, and no Move to group entries', async ({ page }) => {
    await openApp(page);
    const name = await seedCollection(page);
    await reopen(page, name);

    await row(page, 'bravo').click({ button: 'right' });
    await expect(page.locator('.context-menu-item').first()).toBeVisible();
    const items = await page.locator('.context-menu-item').allInnerTexts();
    expect(items.some((i) => i.startsWith('Group'))).toBe(true);
    expect(items).toContain('Remove from group');
    expect(items.some((i) => i.startsWith('Move to “'))).toBe(false);

    await page.locator('.context-menu-item', { hasText: 'Remove from group' }).click();
    await expect.poll(() => layout(page)).toContain('bravo');
  });

  test('the toolbar renames a request: Enter saves, Escape restores, an empty name is ignored', async ({ page }) => {
    await openApp(page);
    const name = await seedCollection(page);
    await reopen(page, name);
    const field = page.locator('.header-name-field');

    await url(page, 'alpha').click();
    await expect(field).toHaveValue('alpha');
    await field.fill('first');
    await field.press('Enter');
    await expect(row(page, 'first')).toHaveCount(1);

    await field.fill('not kept');
    await field.press('Escape');
    await expect(field).toHaveValue('first');
    await expect(row(page, 'not kept')).toHaveCount(0);

    await field.fill('   ');
    await field.press('Enter');
    await expect(field).toHaveValue('first');
  });

  test('a new request puts the cursor in its name in the toolbar', async ({ page }) => {
    await openApp(page);
    const name = await seedCollection(page);
    await reopen(page, name);

    await page.locator('.group-list-header .add-button').first().click();
    await page.locator('.context-menu-item', { hasText: 'New request' }).first().click();
    await expect(page.locator('.header-name-field')).toBeFocused();
    await page.keyboard.type('made here');
    await page.keyboard.press('Enter');
    await expect(row(page, 'made here')).toHaveCount(1);
  });

  test('Ctrl+G and a heading’s Rename put the cursor in the group name in the toolbar', async ({ page }) => {
    await openApp(page);
    const name = await seedCollection(page);
    await reopen(page, name);
    const field = page.locator('.group-tools-name');

    await url(page, 'alpha').click();
    await page.keyboard.press('ControlOrMeta+g');
    await expect(field).toBeFocused();
    await expect(field).toHaveValue('New group');
    await page.keyboard.press('Escape');

    await heading(page, 'Auth').click({ button: 'right', position: { x: 500, y: 15 } });
    await page.locator('.context-menu-item', { hasText: 'Rename' }).click();
    await expect(field).toBeFocused();
    await expect(field).toHaveValue('Auth');
  });

  test('rows hold no edit boxes: a double-click opens zen mode, but not with Ctrl held', async ({ page }) => {
    await openApp(page);
    const name = await seedCollection(page);
    await reopen(page, name);

    await url(page, 'alpha').click();
    await url(page, 'delta').dblclick({ modifiers: ['ControlOrMeta'] });
    await expect(page.locator('app-zen-mode')).toHaveCount(0);

    await row(page, 'alpha').locator('.spreadsheet-cell--name').dblclick();
    await expect(page.locator('.spreadsheet-viewport input')).toHaveCount(0);
    await expect(page.locator('app-zen-mode')).toBeVisible();
  });

  test('a double-click in the trash bin does not open zen mode', async ({ page }) => {
    await openApp(page);
    const name = `binned ${Date.now()}`;
    await page.evaluate(async (name) => {
      const { commands } = window.snaprq;
      const [project] = await commands.execute<{ id: number }[]>('project.list', {});
      const [collection] = await commands.execute<{ id: number }[]>('collection.list', { project_id: project.id });
      const [created] = await commands.execute<{ id: number }[]>('request.create_many', {
        requests: [{ collection_id: collection.id, name, url: 'https://example.com', method: 'GET' }],
      });
      await commands.execute('request.delete', { ids: [created.id] });
    }, name);
    await page.reload();
    await page.waitForFunction(() => window.snaprq?.workspace.view() != null);
    await page.locator('.sidebar-toolbar').getByRole('button', { name: 'Trash bin', exact: true }).click();

    await page.locator('.spreadsheet-row', { hasText: name }).dblclick();
    await expect(page.locator('app-zen-mode')).toHaveCount(0);
  });

  test('a smart view’s bucket folds from its arrow', async ({ page }) => {
    await openApp(page);
    await seedCollection(page);
    await page.reload();
    await page.waitForFunction(() => window.snaprq?.workspace.view() != null);
    await page.locator('.sidebar-toolbar').getByRole('button', { name: 'Smart views', exact: true }).click();
    await page.locator('.smart-view-item', { hasText: 'Not used lately' }).click();

    const bucket = page.locator('.spreadsheet-heading').first();
    await expect(bucket).toBeVisible();
    const before = await page.locator('.spreadsheet-row').count();
    expect(before).toBeGreaterThan(0);
    await bucket.click({ position: { x: 500, y: 15 } });
    expect(await page.locator('.spreadsheet-row').count()).toBe(before);

    await bucket.locator('.fold-toggle').click();
    await expect(page.locator('.spreadsheet-row')).toHaveCount(0);
  });

  test('a folded group stays folded after a reload', async ({ page }) => {
    await openApp(page);
    const name = await seedCollection(page);
    await reopen(page, name);

    await heading(page, 'Users').locator('.fold-toggle').click();
    const folded = START.filter((r) => r !== '  echo' && r !== '  foxtrot');
    await expect.poll(() => layout(page)).toEqual(folded);

    await reopen(page, name);
    expect(await layout(page)).toEqual(folded);
  });
}
