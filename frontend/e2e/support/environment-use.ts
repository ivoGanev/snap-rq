import { expect, test, type Page } from '@playwright/test';
import { clipboardAfter, createRequest, latestResponseBody, openApp, STUB } from './app';

// What the user sees and does around environments and editing: the chosen
// environment is remembered and used, Copy cURL fills it in, and what was
// just typed is what goes out.

const unique = () => `${Date.now()}-${Math.floor(Math.random() * 1e6)}`;

/** Creates environments with plain and secret variables; returns their ids by name. */
async function seedEnvironments(
  page: Page,
  envs: { name: string; plain?: Record<string, string>; secret?: Record<string, string> }[],
): Promise<Record<string, number>> {
  return page.evaluate(async (envs) => {
    const { commands } = window.snaprq;
    const [project] = await commands.execute<{ id: number }[]>('project.list', {});
    const ids: Record<string, number> = {};
    for (const env of envs) {
      const created = await commands.execute<{ id: number }>('environment.create', { project_id: project.id, name: env.name });
      ids[env.name] = created.id;
      for (const [key, value] of Object.entries(env.plain ?? {})) {
        await commands.execute('variable.create', { environment_id: created.id, key, value });
      }
      for (const [key, value] of Object.entries(env.secret ?? {})) {
        const v = await commands.execute<{ id: number }>('variable.create', { environment_id: created.id, key, secret: true });
        await commands.execute('variable.update', { id: v.id, key, value });
      }
    }
    return ids;
  }, envs);
}

async function selectOnProject(page: Page, environmentId: number): Promise<void> {
  await page.evaluate(async (environmentId) => {
    const { commands } = window.snaprq;
    const [project] = await commands.execute<{ id: number }[]>('project.list', {});
    await commands.execute('environment.select', { project_id: project.id, environment_id: environmentId });
  }, environmentId);
}

async function reload(page: Page): Promise<void> {
  await page.reload();
  await page.waitForFunction(() => window.snaprq !== undefined && !window.snaprq.workspace.loading());
}

/** Selects the request through its row, as a click does. */
async function openRow(page: Page, url: string): Promise<void> {
  await page.locator('.spreadsheet-row', { hasText: url }).first().locator('.spreadsheet-cell--name').click();
  await expect(page.locator('.header-url-editor')).toBeVisible();
}

const urlEditor = (page: Page) => page.locator('.header-url-editor .cm-content');

async function replaceText(page: Page, editor: string, text: string): Promise<void> {
  await page.locator(editor).click();
  await page.keyboard.press('Control+A');
  await page.keyboard.type(text);
}

export function environmentUseTests(): void {
  test('the chosen environment is remembered after a reload and is what Send uses', async ({ page }) => {
    await openApp(page);
    const run = unique();
    const alpha = `alpha ${run}`;
    const beta = `beta ${run}`;
    await seedEnvironments(page, [
      { name: alpha, plain: { E2E_ENV: 'alpha' } },
      { name: beta, plain: { E2E_ENV: 'beta' } },
    ]);
    const path = `/echo?env=${run}`;
    const id = await createRequest(page, `${STUB}${path}`, 'X-Env: {{E2E_ENV}}');
    await reload(page);

    const select = page.getByRole('combobox', { name: 'Environment' });
    await select.click();
    await page.getByRole('listbox', { name: 'Environment' }).getByRole('option', { name: beta }).click();
    await expect(select).toHaveText(beta);

    await reload(page);
    await expect(select).toHaveText(beta);

    await openRow(page, path);
    await page.locator('.header-send').getByRole('button').first().click();
    const body = JSON.parse(await latestResponseBody(page, id));
    expect(body.headers['x-env']).toBe('beta');
  });

  test('Copy cURL fills in the environment, secrets included', async ({ page }) => {
    await openApp(page);
    const run = unique();
    const ids = await seedEnvironments(page, [
      { name: `copy ${run}`, plain: { E2E_WHO: 'Ada' }, secret: { E2E_TOKEN: `tok-${run}` } },
    ]);
    await selectOnProject(page, ids[`copy ${run}`]);
    const path = `/echo?copy=${run}&who={{E2E_WHO}}`;
    await createRequest(page, `${STUB}${path}`, 'Authorization: Bearer {{E2E_TOKEN}}');
    await reload(page);

    const curl = await clipboardAfter(page, async () => {
      await page.locator('.spreadsheet-row', { hasText: `copy=${run}` }).first().click({ button: 'right' });
      await page.getByRole('button', { name: 'Copy cURL' }).click();
    });

    expect(curl).toContain(`who=Ada`);
    expect(curl).toContain(`Authorization: Bearer tok-${run}`);
    expect(curl).not.toContain('{{');
  });

  test('what was just typed is what is sent and copied, without waiting', async ({ page }) => {
    await openApp(page);
    const run = unique();
    const id = await createRequest(page, `${STUB}/echo?typed=${run}-old`);
    await reload(page);
    await openRow(page, `typed=${run}-old`);

    await replaceText(page, '.header-url-editor .cm-content', `${STUB}/echo?typed=${run}-new`);
    await urlEditor(page).press('Enter');
    expect(JSON.parse(await latestResponseBody(page, id)).url).toBe(`/echo?typed=${run}-new`);

    await page.locator('.bottom-tab', { hasText: 'Headers' }).click();
    await replaceText(page, 'app-request-headers-tab .cm-content', `X-Typed: ${run}`);
    const before = await latestResponseBody(page, id);
    await page.locator('.header-send').getByRole('button').first().click();
    await expect.poll(() => latestResponseBody(page, id)).not.toBe(before);
    expect(JSON.parse(await latestResponseBody(page, id)).headers['x-typed']).toBe(run);

    await replaceText(page, '.header-url-editor .cm-content', `${STUB}/echo?typed=${run}-copied`);
    const curl = await clipboardAfter(page, async () => {
      await page.locator('.spreadsheet-row', { hasText: `typed=${run}-copied` }).first().click({ button: 'right' });
      await page.getByRole('button', { name: 'Copy cURL' }).click();
    });
    expect(curl).toContain(`typed=${run}-copied`);
  });

  test('the script editor saves as you type', async ({ page }) => {
    await openApp(page);
    const name = `typed script ${unique()}`;
    const requestId = await createRequest(page, `${STUB}/ok?script=${name}`);
    const scriptId = await page.evaluate(
      async ({ name, requestId }) => {
        const { commands } = window.snaprq;
        const [project] = await commands.execute<{ id: number }[]>('project.list', {});
        // Attaching creates the script when the project has none of that name.
        await commands.execute('script.attach', { request_id: requestId, project_id: project.id, script_name: name });
        const scripts = await commands.execute<{ id: number; name: string }[]>('script.list', { project_id: project.id });
        return scripts.find((s) => s.name === name)!.id;
      },
      { name, requestId },
    );
    await reload(page);
    await page.locator('.sidebar-toolbar').getByRole('button', { name: 'Scripts', exact: true }).click();
    // In the test window the request list overlaps the sidebar's list, so
    // the row gets its click directly; what's tested is the saving.
    await page.locator(`[data-item-name="${name}"]`).dispatchEvent('click');
    await expect(page.locator('app-script-editor .cm-content')).toBeVisible();

    await page.locator('app-script-editor .cm-content').click();
    await page.keyboard.type('// saved as typed');

    // Well inside the old 500 ms delay: each change is saved as it happens.
    await expect
      .poll(
        () =>
          page.evaluate(
            async (id) => (await window.snaprq.commands.execute<{ content: string }>('script.get', { id })).content,
            scriptId,
          ),
        { timeout: 400, intervals: [50] },
      )
      .toContain('// saved as typed');
  });

  test('undoing the creation of the selected environment leaves none selected', async ({ page }) => {
    await openApp(page);
    const run = unique();
    const id = await createRequest(page, `${STUB}/echo?undo=${run}`, 'X-Env: {{E2E_UNDO}}');
    const ids = await seedEnvironments(page, [{ name: `undone ${run}`, plain: { E2E_UNDO: 'set' } }]);
    await selectOnProject(page, ids[`undone ${run}`]);
    await reload(page);
    const select = page.getByRole('combobox', { name: 'Environment' });
    await expect(select).toHaveText(`undone ${run}`);

    // Undo steps: the variable, then the environment (selecting isn't one).
    await page.evaluate(async () => {
      await window.snaprq.commands.undo();
      await window.snaprq.commands.undo();
    });
    await expect(select).not.toHaveText(`undone ${run}`);

    await page.evaluate(async (id) => {
      const { requests } = window.snaprq;
      await requests.send(await requests.getFull(id));
    }, id);
    expect(JSON.parse(await latestResponseBody(page, id)).headers['x-env']).toBe('{{E2E_UNDO}}');
  });
}
