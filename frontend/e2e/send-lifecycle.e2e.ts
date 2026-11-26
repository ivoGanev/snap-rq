import { expect, test } from '@playwright/test';
import { STUB, createRequest, expectSettled, inFlight, openApp } from './support/app';

// The lifecycle of a send, end to end: RequestApiService.send() and
// CommandApiService.run() in the page, over the real Wails transport, to the
// runner and request.send in Go, and back as update/finish events.

/** Fails inside Go before any network traffic (an invalid URL escape). */
const INSTANT_FAILURE = 'http://%zz';

test.beforeEach(async ({ page }) => openApp(page));

// Regression: a send that failed fast could finish before Begin's reply
// carried the run id; the finish was dropped and the request stayed
// "Sending… 0 ms" forever. Fifty at once makes that race all but certain.
test('fast failures never stay in flight', async ({ page }) => {
  const id = await createRequest(page, INSTANT_FAILURE);

  const finished = await page.evaluate(async (id) => {
    const { requests } = window.snaprq;
    const req = await requests.getFull(id);
    let finished = 0;
    requests.finished$.subscribe((res) => {
      if (res.request_id === id) finished++;
    });
    await Promise.all(Array.from({ length: 50 }, () => requests.send(req)));
    // Give stragglers a moment, so the count is the same for any send() design.
    await new Promise((r) => setTimeout(r, 500));
    return finished;
  }, id);

  await expectSettled(page, id, 2_000);
  expect(finished, 'every send should report its response').toBe(50);
});

test('a long run reports progress, finishes once, and nothing comes after', async ({ page }) => {
  const id = await createRequest(page, `${STUB}/slow?ms=1500`);

  const run = await page.evaluate(async (id) => {
    const { commands, requests } = window.snaprq;
    const req = await requests.getFull(id);
    let updates = 0;
    const result = await commands.run<{ status_code: number }>(
      'request.send',
      { request: req },
      () => updates++,
    );
    const updatesAtFinish = updates;
    await new Promise((r) => setTimeout(r, 500));
    return { status: result.status_code, updatesAtFinish, updatesAfterFinish: updates - updatesAtFinish };
  }, id);

  expect(run.status).toBe(200);
  expect(run.updatesAtFinish, 'progress while running').toBeGreaterThan(0);
  expect(run.updatesAfterFinish, 'no update after the finish').toBe(0);
});

test('HTTP error statuses finish like any response', async ({ page }) => {
  for (const status of [404, 500]) {
    const id = await createRequest(page, `${STUB}/status/${status}`);
    const code = await page.evaluate(async (id) => {
      const { requests } = window.snaprq;
      const req = await requests.getFull(id);
      let code = -1;
      const sub = requests.finished$.subscribe((res) => {
        if (res.request_id === id) code = res.status_code;
      });
      await requests.send(req);
      sub.unsubscribe();
      return code;
    }, id);

    expect(code).toBe(status);
    await expectSettled(page, id);
  }
});

test('a request stays in flight until its last send finishes', async ({ page }) => {
  const id = await createRequest(page, `${STUB}/slow?ms=1500`);

  await page.evaluate(async (id) => {
    const { requests } = window.snaprq;
    const req = await requests.getFull(id);
    // Same request, sent twice: once slow, once failing at once.
    void requests.send(req);
    await requests.send({ ...req, url: 'http://%zz' });
  }, id);

  expect(await inFlight(page, id), 'the slow send is still running').toBe(true);
  await expectSettled(page, id);
});
