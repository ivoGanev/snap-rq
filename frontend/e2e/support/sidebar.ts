import { expect, test, type Page } from '@playwright/test';
import { openApp } from './app';

// The left sidebar shows one list at a time, picked from its bottom toolbar.
// Opening a list opens its last pick, or its first item, and the picks
// outlive a reload.

const unique = () => `${Date.now()}-${Math.floor(Math.random() * 1e6)}`;

interface Seeded {
  collections: { id: number; name: string }[];
  favourites: { id: number; name: string }[];
  scripts: string[];
}

/** Creates two collections, two favourites and two scripts with unique names. */
async function seed(page: Page): Promise<Seeded> {
  const tag = unique();
  return page.evaluate(async (tag) => {
    const { commands } = window.snaprq;
    const [project] = await commands.execute<{ id: number }[]>('project.list', {});
    const seeded: Seeded = { collections: [], favourites: [], scripts: [] };
    for (const n of [1, 2]) {
      const collectionName = `collection ${tag} ${n}`;
      const collection = await commands.execute<{ id: number }>('collection.create', {
        project_id: project.id,
        name: collectionName,
      });
      seeded.collections.push({ id: collection.id, name: collectionName });
      const favouriteName = `favourite ${tag} ${n}`;
      const favourite = await commands.execute<{ id: number }>('favourite.create', {
        project_id: project.id,
        name: favouriteName,
      });
      seeded.favourites.push({ id: favourite.id, name: favouriteName });
      const scriptName = `script ${tag} ${n}`;
      await commands.execute('script.attach', { request_id: 0, project_id: project.id, script_name: scriptName });
      seeded.scripts.push(scriptName);
    }
    return seeded;
  }, tag);
}

/** Waits until the project is loaded and the sidebar has opened its pick; a click before that is overridden. */
async function sidebarReady(page: Page): Promise<void> {
  await page.waitForFunction(() => window.snaprq?.workspace.view() != null);
}

async function reload(page: Page): Promise<void> {
  await page.reload();
  await sidebarReady(page);
}

const tab = (page: Page, name: string) =>
  page.locator('.sidebar-toolbar').getByRole('button', { name, exact: true });

const item = (page: Page, kind: string, name: string) =>
  page.locator(`.group-list-item[data-item-kind="${kind}"][data-item-name="${name}"]`);

/** The open view's kind and title, as the middle column shows it. */
function openView(page: Page): Promise<{ kind: string; title: string } | null> {
  return page.evaluate(() => {
    const view = window.snaprq.workspace.view();
    return view ? { kind: view.kind, title: view.title } : null;
  });
}

async function expectFirstItemOpen(page: Page, kind: string): Promise<void> {
  const first = page.locator(`.group-list-item[data-item-kind="${kind}"]`).first();
  await expect(first).toHaveClass(/is-selected/);
  const name = await first.getAttribute('data-item-name');
  await expect.poll(() => openView(page)).toEqual({ kind, title: name });
}

export function sidebarTests(): void {
  test('each list opens with its first item, and smart views with All requests', async ({ page }) => {
    await openApp(page);
    await sidebarReady(page);
    await seed(page);
    // Opening the app already saved a pick; this is about having none.
    await page.evaluate(() => localStorage.clear());
    await reload(page);

    await expect(tab(page, 'Collections')).toHaveAttribute('aria-pressed', 'true');
    await expectFirstItemOpen(page, 'collection');

    await tab(page, 'Favourites').click();
    await expectFirstItemOpen(page, 'favourite');

    await tab(page, 'Scripts').click();
    await expectFirstItemOpen(page, 'script');

    await tab(page, 'Smart views').click();
    await expect(page.locator('.smart-view-item.is-selected')).toHaveText('All requests');
    await expect.poll(async () => (await openView(page))?.kind).toBe('all');
  });

  test('the open list and each list’s pick are kept across a reload', async ({ page }) => {
    await openApp(page);
    const { favourites, scripts } = await seed(page);
    await reload(page);

    // In the test window the request list overlaps the sidebar's list, so
    // items get their click directly.
    await tab(page, 'Favourites').click();
    await item(page, 'favourite', favourites[1].name).dispatchEvent('click');
    await tab(page, 'Scripts').click();
    await item(page, 'script', scripts[1]).dispatchEvent('click');
    await expect.poll(async () => (await openView(page))?.title).toBe(scripts[1]);

    await reload(page);
    await expect(tab(page, 'Scripts')).toHaveAttribute('aria-pressed', 'true');
    await expect(item(page, 'script', scripts[1])).toHaveClass(/is-selected/);
    await expect.poll(() => openView(page)).toEqual({ kind: 'script', title: scripts[1] });

    await tab(page, 'Favourites').click();
    await expect(item(page, 'favourite', favourites[1].name)).toHaveClass(/is-selected/);
    await expect.poll(() => openView(page)).toEqual({ kind: 'favourite', title: favourites[1].name });
  });

  test('the picked smart view is kept across a reload', async ({ page }) => {
    await openApp(page);
    await sidebarReady(page);
    await tab(page, 'Smart views').click();
    await page.locator('.smart-view-item', { hasText: 'Failing' }).dispatchEvent('click');
    await expect.poll(async () => (await openView(page))?.kind).toBe('failing');

    await reload(page);
    await expect(tab(page, 'Smart views')).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('.smart-view-item.is-selected')).toHaveText('Failing');
    await expect.poll(async () => (await openView(page))?.kind).toBe('failing');
  });

  test('a picked collection that was deleted falls back to the first one', async ({ page }) => {
    await openApp(page);
    const { collections } = await seed(page);
    await reload(page);

    const picked = collections[1];
    await item(page, 'collection', picked.name).dispatchEvent('click');
    await expect.poll(async () => (await openView(page))?.title).toBe(picked.name);

    await page.evaluate((id) => window.snaprq.commands.execute('collection.delete', { id }), picked.id);
    await reload(page);
    await expect(item(page, 'collection', picked.name)).toHaveCount(0);
    await expectFirstItemOpen(page, 'collection');
  });

  test('the toolbar icons have their size', async ({ page }) => {
    await openApp(page);
    await sidebarReady(page);
    const icon = tab(page, 'Smart views').locator('svg');
    await expect(icon).toHaveCSS('width', '16px');
    await expect(icon).toHaveCSS('height', '16px');
  });
}
