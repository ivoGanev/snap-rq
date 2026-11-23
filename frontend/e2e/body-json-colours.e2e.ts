import { expect, test, type Page } from '@playwright/test';
import { openApp, STUB } from './support/app';

const BODY = '{\n  "name": "Ada",\n  "age": 36,\n  "admin": true,\n  "team": null\n}';

/** The colour a theme variable resolves to, as getComputedStyle reports colours. */
function themeColour(page: Page, variable: string): Promise<string> {
  return page.evaluate((variable) => {
    const probe = document.createElement('span');
    probe.style.color = `var(${variable})`;
    document.body.append(probe);
    const colour = getComputedStyle(probe).color;
    probe.remove();
    return colour;
  }, variable);
}

test("a JSON body is drawn in the theme's syntax colours", async ({ page }) => {
  await openApp(page);
  await page.evaluate(async ({ url, body }) => {
    const { commands, requests } = window.snaprq;
    const [project] = await commands.execute<{ id: number }[]>('project.list', {});
    const [collection] = await commands.execute<{ id: number }[]>('collection.list', { project_id: project.id });
    await requests.create({
      collection_id: collection.id, project_id: project.id, name: 'json body', url, method: 'POST',
      body, body_type: 'json', body_form: '', request_headers: '', status_code: 0, response_id: 0,
    });
  }, { url: `${STUB}/ok`, body: BODY });
  await page.reload();
  await page.waitForFunction(() => window.snaprq !== undefined);

  await page.getByText('json body').first().click();
  await page.locator('.bottom-tab', { hasText: 'Body' }).click();
  const editor = page.locator('app-request-body-tab .cm-content');
  await expect(editor).toContainText('"name"');

  const token = (text: string) => editor.locator('span', { hasText: text }).last();
  await expect(token('"name"')).toHaveCSS('color', await themeColour(page, '--syntax-key'));
  await expect(token('"Ada"')).toHaveCSS('color', await themeColour(page, '--syntax-string'));
  await expect(token('36')).toHaveCSS('color', await themeColour(page, '--syntax-number'));
  await expect(token('true')).toHaveCSS('color', await themeColour(page, '--syntax-boolean'));
  await expect(token('null')).toHaveCSS('color', await themeColour(page, '--syntax-null'));
});
