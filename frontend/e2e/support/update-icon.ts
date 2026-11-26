import { expect, test } from '@playwright/test';
import { openApp } from './app';

export function updateIconTests(): void {
  test('a newer version shows an icon right of Settings that opens Updates', async ({ page }) => {
    await openApp(page);
    const icon = page.getByRole('button', { name: /is available/ });
    await expect(icon).toHaveCount(0);

    // What a check finds; development builds never check GitHub themselves.
    await page.evaluate(() =>
      window.snaprq.updates.available.set({ version: '9.9.9', release_url: 'https://example.com/releases/v9.9.9' }),
    );
    await expect(icon).toBeVisible();
    await expect(icon).toHaveAttribute('title', 'Version 9.9.9 is available');
    const settings = await page.getByRole('button', { name: 'Open settings' }).boundingBox();
    const update = await icon.boundingBox();
    expect(update!.x).toBeGreaterThan(settings!.x);

    await icon.click();
    await expect(page.getByRole('button', { name: 'Updates' })).toHaveAttribute('aria-current', 'page');
    await expect(page.getByText('Version 9.9.9 is available.')).toBeVisible();

    await page.evaluate(() => window.snaprq.updates.available.set(null));
    await expect(icon).toHaveCount(0);
  });
}
