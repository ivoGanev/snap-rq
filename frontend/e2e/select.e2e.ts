import { expect, test } from '@playwright/test';
import { openApp } from './support/app';

// The app draws its own dropdowns: a native <select> opens the operating
// system's list, which only Chromium lets CSS theme (macOS showed its own).
test('a dropdown opens its own list and picks with the mouse or the keyboard', async ({ page }) => {
  await openApp(page);
  await page.evaluate(async () => {
    const { commands } = window.snaprq;
    const [project] = await commands.execute<{ id: number }[]>('project.list', {});
    for (const name of ['Staging', 'Production']) {
      await commands.execute('environment.create', { project_id: project.id, name });
    }
  });
  await page.reload();
  await page.waitForFunction(() => window.snaprq !== undefined);

  const select = page.getByRole('combobox', { name: 'Environment' });
  const list = page.getByRole('listbox', { name: 'Environment' });

  await select.click();
  await expect(list).toBeVisible();
  await list.getByRole('option', { name: 'Staging' }).click();
  await expect(list).toBeHidden();
  await expect(select).toHaveText('Staging');

  await select.press('ArrowDown');
  await expect(select).toHaveAttribute('aria-expanded', 'true');
  await expect(list.getByRole('option', { name: 'Staging' })).toHaveClass(/is-active/);
  await select.press('ArrowDown');
  await expect(list.getByRole('option', { name: 'Production' })).toHaveClass(/is-active/);
  await select.press('Enter');
  await expect(list).toBeHidden();
  await expect(select).toHaveText('Production');

  await select.press('Enter');
  await select.press('ArrowUp');
  await select.press('Escape');
  await expect(list).toBeHidden();
  await expect(select).toHaveText('Production');

  await select.click();
  await expect(list).toBeVisible();
  // The backdrop that catches the outside click fades in over a frame.
  await expect(page.locator('.cdk-overlay-backdrop')).toHaveCSS('visibility', 'visible');
  await page.mouse.click(10, 400);
  await expect(list).toBeHidden();

  await select.press('s');
  await expect(select).toHaveText('Staging');
  await expect(list).toBeHidden();

  // The whole box opens it, padding included, not only the text.
  const box = (await page.locator('app-select.environment-select').boundingBox())!;
  for (const [x, y] of [[box.x + 2, box.y + 2], [box.x + box.width - 2, box.y + box.height - 2]]) {
    await page.mouse.click(x, y);
    await expect(list).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(list).toBeHidden();
  }
});
