import { expect, test, type Page } from '@playwright/test';
import { openApp, STUB } from './app';

/** Creates an environment with one variable and a request, and selects both. */
async function seed(page: Page): Promise<void> {
  const name = `dropdown request ${Date.now()}`;
  await page.evaluate(
    async ({ url, name }) => {
      const { commands, requests } = window.snaprq;
      const [project] = await commands.execute<{ id: number }[]>('project.list', {});
      const [collection] = await commands.execute<{ id: number }[]>('collection.list', {
        project_id: project.id,
      });
      const env = await commands.execute<{ id: number }>('environment.create', {
        project_id: project.id,
        name: 'dropdown',
      });
      await commands.execute('variable.create', { environment_id: env.id, key: 'api-host', value: 'h' });
      await requests.create({
        collection_id: collection.id,
        project_id: project.id,
        name,
        url: `${url}/ok`,
        method: 'GET',
        body: '',
        body_type: 'json',
        body_form: '',
        request_headers: 'X-Team: one',
        status_code: 0,
        response_id: 0,
      });
    },
    { url: STUB, name },
  );
  await page.reload();
  await page.waitForFunction(() => window.snaprq !== undefined);
  await page.evaluate(async () => {
    const { commands, workspace } = window.snaprq;
    const [project] = await commands.execute<{ id: number }[]>('project.list', {});
    const envs = await commands.execute<{ id: number; name: string }[]>('environment.list', {
      project_id: project.id,
    });
    workspace.selectedEnvironment.set(envs.find((e) => e.name === 'dropdown') as never);
  });
  await page.getByText(name).first().click();
}

/**
 * Presses Ctrl+Space at the end of the editor's text and checks the variables
 * dropdown hangs just under the caret, not shifted by a blurred ancestor.
 */
async function expectDropdownUnderCaret(page: Page, editor: string): Promise<void> {
  await page.locator(`${editor} .cm-content`).click();
  await page.keyboard.press('End');
  await page.keyboard.press('Control+Space');

  const dropdown = page.locator('.variable-dropdown');
  await expect(dropdown).toBeVisible();
  await expect(dropdown).toContainText('api-host');

  const caret = await page.locator(`${editor} .cm-cursor`).first().boundingBox();
  const box = await dropdown.boundingBox();
  expect(caret && box).toBeTruthy();
  expect(Math.abs(box!.x - caret!.x)).toBeLessThanOrEqual(2);
  expect(box!.y - (caret!.y + caret!.height)).toBeGreaterThanOrEqual(0);
  expect(box!.y - (caret!.y + caret!.height)).toBeLessThanOrEqual(8);

  await page.keyboard.press('Escape');
  await expect(dropdown).toHaveCount(0);
}

export function variableDropdownTests(): void {
  test('Ctrl+Space opens the variables dropdown under the caret in the URL bar and the drawer', async ({
    page,
  }) => {
    await openApp(page);
    await seed(page);

    await expectDropdownUnderCaret(page, '.header-url-editor');

    await page.locator('.bottom-tab', { hasText: 'Headers' }).click();
    await expectDropdownUnderCaret(page, 'app-request-headers-tab');
  });
}
