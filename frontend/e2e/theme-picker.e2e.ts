import { expect, test } from '@playwright/test';
import { openApp } from './support/app';

// A click-outside backdrop placed inside a bar with backdrop-filter shrinks
// to that bar, and clicks elsewhere leave the picker open. Only a real
// browser's layout shows this.
test('the theme picker closes when clicking outside it', async ({ page }) => {
  await openApp(page);
  await page.getByRole('button', { name: 'Choose theme' }).click();
  const picker = page.locator('.theme-selector-popup');
  await expect(picker).toBeVisible();

  // A click in the middle of the window, away from the top bar, like a
  // user's: the browser decides what is under it.
  const { width, height } = page.viewportSize()!;
  await page.mouse.click(width / 2, height / 2);

  await expect(picker).toBeHidden();
});
