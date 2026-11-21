import { expect, test } from '@playwright/test';
import { openApp } from './support/app';

// Regression: the click-outside backdrop sat inside the footer, whose
// backdrop-filter shrank it to the footer's 32px, so clicking anywhere above
// the footer left the picker open. Only a real browser's layout shows this.
test('the theme picker closes when clicking outside it', async ({ page }) => {
  await openApp(page);
  await page.getByRole('button', { name: 'Choose theme' }).click();
  const picker = page.locator('.theme-selector-popup');
  await expect(picker).toBeVisible();

  // A click at a point in the middle of the window, well above the footer,
  // like a user's: the browser decides what is under it.
  const { width, height } = page.viewportSize()!;
  await page.mouse.click(width / 2, height / 2);

  await expect(picker).toBeHidden();
});
