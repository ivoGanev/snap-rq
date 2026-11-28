import { expect, test, type Locator, type Page } from '@playwright/test';
import { openApp } from './app';

// Request groups in a collection: requests and groups share one order set by
// dragging, a group's requests sit indented under its heading, a heading
// folds on click, and a group that a drag empties is removed.

export const unique = () => `${Date.now()}-${Math.floor(Math.random() * 1e6)}`;
const NAMES = ['alpha', 'bravo', 'charlie', 'delta', 'echo', 'foxtrot', 'golf', 'hotel'];

/**
 * A collection laid out as:
 *   alpha, [Auth: bravo, charlie], delta, [Users: echo, foxtrot], golf, hotel
 */
export async function seedCollection(page: Page): Promise<string> {
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

export async function openCollection(page: Page, name: string): Promise<void> {
  await page.waitForFunction(() => window.snaprq?.workspace.view() != null);
  await page.locator(`.group-list-item[data-item-kind="collection"][data-item-name="${name}"]`).dispatchEvent('click');
  await expect(row(page, 'alpha')).toBeVisible();
}

export async function reopen(page: Page, name: string): Promise<void> {
  await page.reload();
  await openCollection(page, name);
}

/** The list as drawn: "# Group" for a heading, "  name" for a request inside a group. */
export function layout(page: Page): Promise<string[]> {
  return page.evaluate(() =>
    [...document.querySelectorAll('.spreadsheet-heading, .spreadsheet-row')].map((el) =>
      el.classList.contains('spreadsheet-heading')
        ? `# ${el.querySelector('.spreadsheet-heading-label')?.textContent?.trim()}`
        : `${el.classList.contains('is-nested') ? '  ' : ''}${el.querySelector('.spreadsheet-cell--name')?.textContent?.trim()}`,
    ),
  );
}

const exactly = (text: string) => new RegExp(`^${text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}$`);

export const row = (page: Page, name: string) =>
  page.locator('.spreadsheet-row').filter({ has: page.locator('.spreadsheet-cell--name', { hasText: exactly(name) }) });
export const heading = (page: Page, name: string) => page.locator('.spreadsheet-heading', { hasText: name });

export const selectedNames = (page: Page) =>
  page.evaluate(() =>
    [...document.querySelectorAll('.spreadsheet-row.is-selected .spreadsheet-cell--name')].map((e) => e.textContent?.trim()),
  );

export type Point = { x: number; y: number };

/** The middle of an element; `dx` from its left edge. */
export async function middle(locator: Locator, dx = 300): Promise<Point> {
  const box = (await locator.boundingBox())!;
  return { x: box.x + dx, y: box.y + box.height / 2 };
}

/** Just above an element's bottom edge, which is the boundary below it. */
export async function bottomEdge(locator: Locator, dx = 300): Promise<Point> {
  const box = (await locator.boundingBox())!;
  return { x: box.x + dx, y: box.y + box.height - 2 };
}

/** Presses and moves in steps (as a hand does), keeping the button down. */
export async function press(page: Page, from: Point, to: Point): Promise<void> {
  await page.mouse.move(from.x, from.y);
  await page.mouse.down();
  for (let i = 1; i <= 10; i++) {
    await page.mouse.move(from.x + ((to.x - from.x) * i) / 10, from.y + ((to.y - from.y) * i) / 10);
  }
}

export async function drag(page: Page, from: Point, to: Point): Promise<void> {
  await press(page, from, to);
  await page.mouse.up();
}

/** The list's top edge: it must not move when the URL bar's contents change. */
export async function listTop(page: Page): Promise<number> {
  return (await page.locator('.spreadsheet-wrapper').boundingBox())!.y;
}

export const START = ['alpha', '# Auth', '  bravo', '  charlie', 'delta', '# Users', '  echo', '  foxtrot', 'golf', 'hotel'];

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

  test('undo after dragging a group empty brings the group and the order back', async ({ page }) => {
    await openApp(page);
    const name = await seedCollection(page);
    await reopen(page, name);

    await row(page, 'bravo').locator('.spreadsheet-cell--url').click();
    await row(page, 'charlie').locator('.spreadsheet-cell--url').click({ modifiers: ['ControlOrMeta'] });
    const top = (await row(page, 'alpha').boundingBox())!;
    await drag(page, await middle(row(page, 'charlie')), { x: top.x + 60, y: top.y + 2 });
    await expect.poll(() => layout(page)).not.toContain('# Auth');

    await page.evaluate(() => window.snaprq.commands.undo());
    await expect.poll(() => layout(page)).toEqual(START);
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

  test('the arrow folds and unfolds a group; a click elsewhere on the heading only picks it', async ({ page }) => {
    await openApp(page);
    const name = await seedCollection(page);
    await reopen(page, name);

    await heading(page, 'Auth').click({ position: { x: 500, y: 15 } });
    await expect(page.locator('.group-tools-name')).toHaveValue('Auth');
    expect(await layout(page)).toEqual(START);

    await heading(page, 'Auth').locator('.fold-toggle').click();
    await expect.poll(() => layout(page)).toEqual(START.filter((r) => r !== '  bravo' && r !== '  charlie'));
    await heading(page, 'Auth').locator('.fold-toggle').click();
    await expect.poll(() => layout(page)).toEqual(START);
  });

  test('folding a group swaps the stripes below it at once, without a fade', async ({ page }) => {
    await openApp(page);
    const name = await seedCollection(page);
    await reopen(page, name);
    // A third request in Auth: folding it moves the rows below by an odd count, so their stripes swap.
    await drag(page, await middle(row(page, 'delta')), await bottomEdge(row(page, 'charlie')));
    await expect.poll(() => layout(page)).toContain('  delta');

    const look = () =>
      page.evaluate(() => {
        const golf = [...document.querySelectorAll('.spreadsheet-row')].find((r) =>
          r.querySelector('.spreadsheet-cell--name')?.textContent?.trim() === 'golf',
        )!;
        const style = getComputedStyle(golf);
        return `${style.backgroundColor} ${style.backgroundImage}`;
      });
    await page.mouse.move(5, 5);
    const before = await look();

    await heading(page, 'Auth').locator('.fold-toggle').click();
    await page.mouse.move(5, 5);
    const samples: string[] = [];
    for (let i = 0; i < 6; i++) {
      samples.push(await look());
      await page.waitForTimeout(20);
    }
    await page.waitForTimeout(300);
    const after = await look();

    expect(after).not.toBe(before);
    expect(samples.every((s) => s === after)).toBe(true);
  });

  test('dragging a group does not select a request', async ({ page }) => {
    await openApp(page);
    const name = await seedCollection(page);
    await reopen(page, name);

    // Picking a group clears the request selection.
    await heading(page, 'Users').click({ position: { x: 500, y: 15 } });
    await expect.poll(() => selectedNames(page)).toEqual([]);

    await drag(page, await middle(heading(page, 'Users')), await bottomEdge(row(page, 'alpha')));
    await expect.poll(() => layout(page)).toContain('# Users');
    await expect.poll(async () => (await layout(page)).indexOf('# Users')).toBe(1);
    expect(await selectedNames(page)).toEqual([]);
  });

  test('a group dragged a little down shows no drop line and stays in place', async ({ page }) => {
    await openApp(page);
    const name = await seedCollection(page);
    await reopen(page, name);

    await press(page, await middle(heading(page, 'Users')), await bottomEdge(row(page, 'foxtrot')));
    await page.waitForTimeout(100);
    await expect(page.locator('.reorder-drop-line')).toBeHidden();
    await page.mouse.up();

    await page.waitForTimeout(300);
    expect(await layout(page)).toEqual(START);
  });

  test('a picked group shows in the URL bar and leaves the inspector empty', async ({ page }) => {
    await openApp(page);
    const name = await seedCollection(page);
    await reopen(page, name);

    await row(page, 'alpha').locator('.spreadsheet-cell--url').click();
    await expect(page.locator('.header-url-field')).toBeVisible();

    await heading(page, 'Auth').click({ position: { x: 500, y: 15 } });
    await expect(page.locator('app-response-pane')).toHaveCount(0);
    await expect.poll(async () => (await page.locator('app-inspector').innerText()).trim()).toBe('');
    await expect(page.locator('.group-tools-name')).toHaveValue('Auth');
    // Picking a group must not put the cursor in its name: typing would rename it.
    await expect(page.locator('.group-tools-name')).not.toBeFocused();
  });

  test('the URL bar renames, colours and ungroups a picked group', async ({ page }) => {
    await openApp(page);
    const name = await seedCollection(page);
    await reopen(page, name);

    await heading(page, 'Auth').click({ position: { x: 500, y: 15 } });
    await page.locator('.group-tools-name').fill('Login');
    await page.locator('.group-tools-name').press('Enter');
    await expect(heading(page, 'Login')).toBeVisible();

    await page.locator('.group-tools .color-dot:not(.color-dot--clear)').nth(1).click();
    await expect(heading(page, 'Login')).toHaveClass(/has-color/);
    await expect(row(page, 'bravo')).toHaveClass(/in-coloured-group/);

    await page.locator('.group-tools .selection-tool', { hasText: 'Ungroup' }).click();
    await expect
      .poll(() => layout(page))
      .toEqual(['alpha', 'bravo', 'charlie', 'delta', '# Users', '  echo', '  foxtrot', 'golf', 'hotel']);
  });

  test('the list keeps its place whether a request, a group or nothing is selected', async ({ page }) => {
    await openApp(page);
    const name = await seedCollection(page);
    await reopen(page, name);

    await row(page, 'alpha').locator('.spreadsheet-cell--url').click();
    const withRequest = await listTop(page);
    await heading(page, 'Auth').click({ position: { x: 500, y: 15 } });
    await expect(page.locator('.group-tools')).toBeVisible();
    const withGroup = await listTop(page);
    await row(page, 'delta').locator('.spreadsheet-cell--url').click();
    await row(page, 'delta').locator('.spreadsheet-cell--url').click({ modifiers: ['ControlOrMeta'] });
    await expect(page.locator('.header-hint')).toBeVisible();
    const withNothing = await listTop(page);

    expect(withGroup).toBe(withRequest);
    expect(withNothing).toBe(withRequest);
  });

  test('dragging a request out of a group stays quick in a large collection', async ({ page }) => {
    await openApp(page);
    const name = `large ${unique()}`;
    const collectionId = await page.evaluate(
      async ({ name, total, grouped }) => {
        const { commands } = window.snaprq;
        const [project] = await commands.execute<{ id: number }[]>('project.list', {});
        const collection = await commands.execute<{ id: number }>('collection.create', { project_id: project.id, name });
        const requests = Array.from({ length: total }, (_, i) => ({
          collection_id: collection.id,
          name: `req ${String(i).padStart(4, '0')}`,
          url: `https://example.com/${i}`,
          method: 'GET',
        }));
        const created = await commands.execute<{ id: number }[]>('request.create_many', { requests });
        await commands.execute('group.create', {
          collection_id: collection.id,
          name: 'Big group',
          request_ids: created.slice(0, grouped).map((r) => r.id),
        });
        return collection.id;
      },
      { name, total: 3000, grouped: 1000 },
    );

    try {
      await page.reload();
      await page.waitForFunction(() => window.snaprq?.workspace.view() != null);
      await page.locator(`.group-list-item[data-item-kind="collection"][data-item-name="${name}"]`).dispatchEvent('click');
      const first = row(page, 'req 0000');
      await expect(first).toHaveClass(/is-nested/);

      // The longest the page goes without drawing a frame is how long it freezes.
      const longestFreeze = page.evaluate(
        () =>
          new Promise<number>((resolve) => {
            let last = performance.now();
            let longest = 0;
            const end = last + 3000;
            const tick = (now: number) => {
              longest = Math.max(longest, now - last);
              last = now;
              if (now < end) requestAnimationFrame(tick);
              else resolve(longest);
            };
            requestAnimationFrame(tick);
          }),
      );

      // Above the group's heading: out of the group, at the top of the list.
      const top = (await heading(page, 'Big group').boundingBox())!;
      await press(page, await middle(first), { x: top.x + 60, y: top.y + 2 });
      await page.mouse.up();
      await expect(first).not.toHaveClass(/is-nested/, { timeout: 20_000 });
      // Writing the request store once per row froze it for seconds at this size.
      const freeze = await longestFreeze;
      expect(freeze).toBeLessThan(500);
    } finally {
      await page.evaluate((id) => window.snaprq.commands.execute('collection.delete', { id }), collectionId);
    }
  });
}

