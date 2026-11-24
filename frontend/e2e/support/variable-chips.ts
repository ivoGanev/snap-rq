import { expect, test, type Page } from '@playwright/test';
import { openApp, STUB } from './app';

const NAME = 'fleet-management.v2';

/** Creates an environment holding NAME and a request using it in its URL, a header and the body. */
async function seed(page: Page): Promise<void> {
  await page.evaluate(
    async ({ url, name }) => {
      const { commands, requests } = window.snaprq;
      const [project] = await commands.execute<{ id: number }[]>('project.list', {});
      const [collection] = await commands.execute<{ id: number }[]>('collection.list', {
        project_id: project.id,
      });
      const env = await commands.execute<{ id: number }>('environment.create', {
        project_id: project.id,
        name: 'chips',
      });
      await commands.execute('variable.create', { environment_id: env.id, key: name, value: 'fm' });
      await requests.create({
        collection_id: collection.id,
        project_id: project.id,
        name: 'chip request',
        url: `${url}/{{${name}}}`,
        method: 'POST',
        body: `{"team": "{{ ${name} }}"}`,
        body_type: 'json',
        body_form: '',
        request_headers: `X-Team: {{${name}}}`,
        status_code: 0,
        response_id: 0,
      });
    },
    { url: STUB, name: NAME },
  );
  await page.reload();
  await page.waitForFunction(() => window.snaprq !== undefined);
  await page.evaluate(async () => {
    const { commands, workspace } = window.snaprq;
    const [project] = await commands.execute<{ id: number }[]>('project.list', {});
    const envs = await commands.execute<{ id: number; name: string }[]>('environment.list', {
      project_id: project.id,
    });
    workspace.selectedEnvironment.set(envs.find((e) => e.name === 'chips') as never);
  });
}

export function variableChipTests(): void {
  test('a variable with - and . in its name shows as a chip in the URL, headers and body', async ({
    page,
  }) => {
    await openApp(page);
    await seed(page);
    await page.getByText('chip request').first().click();

    const chip = (editor: string) => page.locator(`${editor} .variable-chip`);
    await expect(chip('.header-url-editor')).toHaveCount(1);
    await expect(chip('.header-url-editor')).not.toHaveClass(/is-missing/);

    await page.locator('.bottom-tab', { hasText: 'Headers' }).click();
    await expect(chip('app-request-headers-tab')).toHaveCount(1);

    await page.locator('.bottom-tab', { hasText: 'Body' }).click();
    await expect(chip('app-request-body-tab')).toHaveCount(1);
  });

  test('the variables editor refuses a name {{…}} could never fill in', async ({ page }) => {
    await openApp(page);
    await seed(page);
    await page.getByRole('button', { name: 'Edit environment variables' }).click();
    const keys = page.locator('.variables-cell--key input');
    await expect(keys).toHaveCount(1);
    await page.locator('.variable-add-button').click();
    await expect(keys).toHaveCount(2);

    // The new row's place in the list can change once it loads, so it's found by its state.
    await page.locator('.variables-cell--key input:placeholder-shown').fill('a b');
    const invalid = page.locator('.variables-cell--key input.is-invalid');
    await expect(invalid).toHaveValue('a b');
    await expect(page.locator('.variables-key-error')).toBeVisible();

    await invalid.fill('a-b.c');
    await expect(page.locator('.variables-cell--key input.is-invalid')).toHaveCount(0);
    await expect(page.locator('.variables-key-error')).toHaveCount(0);
  });
}
