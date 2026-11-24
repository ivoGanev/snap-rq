import { expect, test, type Page } from '@playwright/test';
import { openApp } from './app';

// index.html picks the platform from the user agent.
const MAC_AGENT =
  'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15';
const WINDOWS_AGENT =
  'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36';

// Where macOS draws the traffic lights over the bar's left end.
const TRAFFIC_LIGHTS_WIDTH = 78;

async function topBarBox(page: Page) {
  const box = await page.locator('.top-bar').boundingBox();
  expect(box).not.toBeNull();
  return box!;
}

/** The top bar is the window's title bar: flush with the top and spanning the width. */
export function topBarTests(): void {
  test.describe('on macOS', () => {
    test.use({ userAgent: MAC_AGENT });

    test('the top bar sits at the top, clear of the traffic lights', async ({ page }) => {
      await openApp(page);
      const width = page.viewportSize()!.width;
      const bar = await topBarBox(page);
      expect(bar).toMatchObject({ x: 0, y: 0, width, height: 40 });

      const toggle = await page.locator('.top-bar-sidebar-toggle').boundingBox();
      expect(toggle!.x).toBeGreaterThanOrEqual(TRAFFIC_LIGHTS_WIDTH);
      await expect(page.locator('app-window-controls')).toHaveCount(0);
    });
  });

  test.describe('on Windows', () => {
    test.use({ userAgent: WINDOWS_AGENT });

    test('the top bar sits at the top, with the window controls at its right end', async ({ page }) => {
      await openApp(page);
      const width = page.viewportSize()!.width;
      const bar = await topBarBox(page);
      expect(bar).toMatchObject({ x: 0, y: 0, width, height: 40 });

      const close = await page.getByRole('button', { name: 'Close', exact: true }).boundingBox();
      expect(close!.x + close!.width).toBeCloseTo(width, 0);
      expect(close!.y).toBe(0);
    });
  });
}
