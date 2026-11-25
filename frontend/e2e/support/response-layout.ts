import { expect, test, type Locator, type Page } from '@playwright/test';
import { openApp, openResponse } from './app';

const pane = (page: Page) => page.locator('app-response-pane');
const scroller = (page: Page) => pane(page).locator('app-response-text .cm-scroller');

async function box(locator: Locator) {
  const b = await locator.boundingBox();
  if (!b) throw new Error('not visible');
  return b;
}

/** The number in the first line-number cell drawn in the visible part of the body. */
function firstVisibleLine(page: Page): Promise<string> {
  return scroller(page).evaluate((el) => {
    const top = el.getBoundingClientRect().top;
    const cells = [...el.querySelectorAll('.cm-lineNumbers .cm-gutterElement')] as HTMLElement[];
    const cell = cells.find((c) => c.textContent && c.getBoundingClientRect().bottom > top + 1);
    return cell?.textContent ?? '';
  });
}

export function responseLayoutTests(): void {
  test('only the body scrolls, and its scrollbars sit on the panel bottom', async ({ page }) => {
    await openApp(page);
    await openResponse(page, '/long');
    const status = pane(page).locator('.response-status');
    const bodyHeading = pane(page).locator('.section-toggle', { hasText: 'Body' });
    await expect(pane(page).locator('app-response-text .cm-content')).toContainText('"items"');
    const before = { status: await box(status), heading: await box(bodyHeading) };

    await scroller(page).evaluate((el) => {
      el.scrollTop = 3000;
      el.scrollLeft = 400;
    });

    const scrolled = await scroller(page).evaluate((el) => ({
      top: el.scrollTop,
      left: el.scrollLeft,
      wide: el.scrollWidth > el.clientWidth,
    }));
    expect(scrolled.top).toBeGreaterThan(0);
    expect(scrolled.left).toBeGreaterThan(0);
    expect(scrolled.wide).toBe(true);
    expect(await box(status)).toEqual(before.status);
    expect(await box(bodyHeading)).toEqual(before.heading);
    expect(await pane(page).locator('.response-headers').evaluate((el) => el.scrollLeft)).toBe(0);

    const panelBottom = await pane(page)
      .locator('.response-detail')
      .evaluate((el) => el.getBoundingClientRect().bottom - parseFloat(getComputedStyle(el).paddingBottom));
    const body = await box(scroller(page));
    expect(Math.abs(panelBottom - (body.y + body.height))).toBeLessThanOrEqual(2);
  });

  test('revealing a secret keeps the body where it was scrolled to', async ({ page }) => {
    await openApp(page);
    await openResponse(page, '/long');
    const content = pane(page).locator('app-response-text .cm-content');
    await expect(content).toContainText('"items"');

    // Scroll until the secret far down is drawn.
    await scroller(page).evaluate(async (el) => {
      const drawn = () => el.querySelector('.cm-content')?.textContent?.includes('"access_token": "•••••"');
      for (let i = 0; i < 500 && !drawn(); i++) {
        el.scrollTop += el.clientHeight / 2;
        await new Promise((done) => requestAnimationFrame(() => requestAnimationFrame(done)));
      }
    });
    await expect(content).toContainText('"access_token": "•••••"');
    const where = {
      top: await scroller(page).evaluate((el) => el.scrollTop),
      line: await firstVisibleLine(page),
    };

    await pane(page).locator('.icon-button[aria-label="Reveal secrets in the body"]').click();

    await expect(content).toContainText('tok-deep-7');
    expect(await scroller(page).evaluate((el) => el.scrollTop)).toBe(where.top);
    expect(await firstVisibleLine(page)).toBe(where.line);
  });
}
