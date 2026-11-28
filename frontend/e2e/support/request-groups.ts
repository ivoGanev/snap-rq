import { expect, test, type Locator, type Page } from '@playwright/test';
import { openApp } from './app';

// Request groups in a collection: requests and groups share one order set by
// dragging, a group's requests sit indented under its heading, a heading
// folds on click, and a group that a drag empties is removed.

const unique = () => `${Date.now()}-${Math.floor(Math.random() * 1e6)}`;
const NAMES = ['alpha', 'bravo', 'charlie', 'delta', 'echo', 'foxtrot', 'golf', 'hotel'];

/**
 * A collection laid out as:
 *   alpha, [Auth: bravo, charlie], delta, [Users: echo, foxtrot], golf, hotel
 */
async function seedCollection(page: Page): Promise<string> {
  const name = `groups ${unique()}`;
  await page.evaluate(
    async ({ name, names }) => {
      const { commands, requests } = window.snaprq;
      const [project] = await commands.execute<{ id: number }[]>('project.list', {});
      const collection = await commands.execute<{ id: number }>('collection.create', { project_id: project.id, name });
      const ids: Record<string, number> = {};
      for (const n of names) {
        const created = await requests.create({
          collection_id: collection.id,
          project_id: project.id,
          name: n,
          url: `https://example.com/${n}`,
          method: 'GET',
          body: '',
          body_type: 'json',
          body_form: '',
          request_headers: '',
          status_code: 0,
          response_id: 0,
        });
        ids[n] = created.id;
      }
      await commands.execute('group.create', { collection_id: collection.id, name: 'Auth', request_ids: [ids['bravo'], ids['charlie']] });
      await commands.execute('group.create', { collection_id: collection.id, name: 'Users', request_ids: [ids['echo'], ids['foxtrot']] });
    },
    { name, names: NAMES },
  );
  return name;
}

async function openCollection(page: Page, name: string): Promise<void> {
  await page.waitForFunction(() => window.snaprq?.workspace.view() != null);
  await page.locator(`.group-list-item[data-item-kind="collection"][data-item-name="${name}"]`).dispatchEvent('click');
  await expect(row(page, 'alpha')).toBeVisible();
}

async function reopen(page: Page, name: string): Promise<void> {
  await page.reload();
  await openCollection(page, name);
}

/** The list as drawn: "# Group" for a heading, "  name" for a request inside a group. */
function layout(page: Page): Promise<string[]> {
  return page.evaluate(() =>
    [...document.querySelectorAll('.spreadsheet-heading, .spreadsheet-row')].map((el) =>
      el.classList.contains('spreadsheet-heading')
        ? `# ${el.querySelector('.spreadsheet-heading-label')?.textContent?.trim()}`
        : `${el.classList.contains('is-nested') ? '  ' : ''}${el.querySelector('.spreadsheet-cell--name')?.textContent?.trim()}`,
    ),
  );
}

const row = (page: Page, name: string) =>
  page.locator('.spreadsheet-row').filter({ has: page.locator('.spreadsheet-cell--name', { hasText: new RegExp(`^${name}$`) }) });
const heading = (page: Page, name: string) => page.locator('.spreadsheet-heading', { hasText: name });

const selectedNames = (page: Page) =>
  page.evaluate(() =>
    [...document.querySelectorAll('.spreadsheet-row.is-selected .spreadsheet-cell--name')].map((e) => e.textContent?.trim()),
  );

type Point = { x: number; y: number };

/** The middle of an element; `dx` from its left edge. */
async function middle(locator: Locator, dx = 300): Promise<Point> {
  const box = (await locator.boundingBox())!;
  return { x: box.x + dx, y: box.y + box.height / 2 };
}

/** Just above an element's bottom edge, which is the boundary below it. */
async function bottomEdge(locator: Locator, dx = 300): Promise<Point> {
  const box = (await locator.boundingBox())!;
  return { x: box.x + dx, y: box.y + box.height - 2 };
}

/** Presses, moves in steps (as a hand does) and releases. */
async function drag(page: Page, from: Point, to: Point): Promise<void> {
  await page.mouse.move(from.x, from.y);
  await page.mouse.down();
  for (let i = 1; i <= 10; i++) {
    await page.mouse.move(from.x + ((to.x - from.x) * i) / 10, from.y + ((to.y - from.y) * i) / 10);
  }
  await page.mouse.up();
}

const START = ['alpha', '# Auth', '  bravo', '  charlie', 'delta', '# Users', '  echo', '  foxtrot', 'golf', 'hotel'];

export function requestGroupsTests(): void {
  test.use({ viewport: { width: 1400, height: 900 } });

  test('a collection shows its groups in place, their requests indented', async ({ page }) => {
    await openApp(page);
    const name = await seedCollection(page);
    await reopen(page, name);
    expect(await layout(page)).toEqual(START);
  });

  test('a request dragged between two rows moves there, and stays after a reload', async ({ page }) => {
    await openApp(page);
    const name = await seedCollection(page);
    await reopen(page, name);

    await drag(page, await middle(row(page, 'golf')), await bottomEdge(row(page, 'alpha')));

    const moved = ['alpha', 'golf', '# Auth', '  bravo', '  charlie', 'delta', '# Users', '  echo', '  foxtrot', 'hotel'];
    await expect.poll(() => layout(page)).toEqual(moved);
    await reopen(page, name);
    expect(await layout(page)).toEqual(moved);
  });

  test('a request dragged between two of a group’s requests joins the group there', async ({ page }) => {
    await openApp(page);
    const name = await seedCollection(page);
    await reopen(page, name);

    await drag(page, await middle(row(page, 'hotel')), await bottomEdge(row(page, 'bravo')));

    await expect
      .poll(() => layout(page))
      .toEqual(['alpha', '# Auth', '  bravo', '  hotel', '  charlie', 'delta', '# Users', '  echo', '  foxtrot', 'golf']);
  });

  test('a request dropped on a heading joins the end of that group', async ({ page }) => {
    await openApp(page);
    const name = await seedCollection(page);
    await reopen(page, name);

    await drag(page, await middle(row(page, 'golf')), await middle(heading(page, 'Users')));

    await expect
      .poll(() => layout(page))
      .toEqual(['alpha', '# Auth', '  bravo', '  charlie', 'delta', '# Users', '  echo', '  foxtrot', '  golf', 'hotel']);
  });

  test('dragging all of a group’s requests out removes the group', async ({ page }) => {
    await openApp(page);
    const name = await seedCollection(page);
    await reopen(page, name);

    await row(page, 'bravo').locator('.spreadsheet-cell--url').click();
    await row(page, 'charlie').locator('.spreadsheet-cell--url').click({ modifiers: ['ControlOrMeta'] });
    const top = (await row(page, 'alpha').boundingBox())!;
    await drag(page, await middle(row(page, 'charlie')), { x: top.x + 60, y: top.y + 2 });

    const expected = ['bravo', 'charlie', 'alpha', 'delta', '# Users', '  echo', '  foxtrot', 'golf', 'hotel'];
    await expect.poll(() => layout(page)).toEqual(expected);
    await reopen(page, name);
    expect(await layout(page)).toEqual(expected);
  });

  test('a group dragged by its heading moves with its requests', async ({ page }) => {
    await openApp(page);
    const name = await seedCollection(page);
    await reopen(page, name);

    await drag(page, await middle(heading(page, 'Users')), await bottomEdge(row(page, 'alpha')));

    const moved = ['alpha', '# Users', '  echo', '  foxtrot', '# Auth', '  bravo', '  charlie', 'delta', 'golf', 'hotel'];
    await expect.poll(() => layout(page)).toEqual(moved);
    await reopen(page, name);
    expect(await layout(page)).toEqual(moved);
  });

  test('a click anywhere on a heading folds and unfolds its group', async ({ page }) => {
    await openApp(page);
    const name = await seedCollection(page);
    await reopen(page, name);

    await heading(page, 'Auth').click({ position: { x: 500, y: 15 } });
    await expect.poll(() => layout(page)).toEqual(START.filter((r) => r !== '  bravo' && r !== '  charlie'));
    await heading(page, 'Auth').click({ position: { x: 500, y: 15 } });
    await expect.poll(() => layout(page)).toEqual(START);
  });

  test('dragging a group does not select a request', async ({ page }) => {
    await openApp(page);
    const name = await seedCollection(page);
    await reopen(page, name);

    // Picking a group clears the request selection.
    await heading(page, 'Users').click({ position: { x: 500, y: 15 } });
    await heading(page, 'Users').click({ position: { x: 500, y: 15 } });
    await expect.poll(() => selectedNames(page)).toEqual([]);

    await drag(page, await middle(heading(page, 'Users')), await bottomEdge(row(page, 'alpha')));
    await expect.poll(() => layout(page)).toContain('# Users');
    await expect.poll(async () => (await layout(page)).indexOf('# Users')).toBe(1);
    expect(await selectedNames(page)).toEqual([]);
  });
}
