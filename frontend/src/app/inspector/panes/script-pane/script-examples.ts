/** One example in the script reference: a tab with a note and its code. */
export interface ScriptExample {
  id: string;
  /** The tab's label. */
  title: string;
  /** What it does, in one line. */
  note: string;
  code: string;
}

/**
 * The reference's examples. They only use what scripts can do today (see
 * backend/scripting); keep them in sync with the API.
 */
export const SCRIPT_EXAMPLES: ScriptExample[] = [
  {
    id: 'auth',
    title: 'Auth',
    note: "Adds a bearer token from the selected environment to every request it's attached to.",
    code: `function beforeRequest(req, ctx) {
  const token = ctx.env.get('TOKEN');
  req.headers.set('Authorization',
    'Bearer ' + token);
}`,
  },
  {
    id: 'tests',
    title: 'Tests',
    note: 'Checks the status, a JSON field, a header and the time taken. Each shows ✓ or ✗ under Scripts.',
    code: `function afterResponse(res, ctx) {
  ctx.test('status is 200', () =>
    ctx.expect(res.status).toBe(200));

  ctx.test('returns an id', () =>
    ctx.expect(res.json().id)
      .toBeTruthy());

  ctx.test('answers in JSON', () => {
    const type =
      res.headers.get('Content-Type');
    ctx.expect(type)
      .toContain('application/json');
  });

  ctx.test('takes under 500 ms', () =>
    ctx.expect(res.durationMs)
      .toBeLessThan(500));
}`,
  },
  {
    id: 'body',
    title: 'Change body',
    note: 'Edits the JSON body before sending: adds a timestamp and a header.',
    code: `function beforeRequest(req, ctx) {
  const body = JSON.parse(req.body || '{}');
  body.sentAt = new Date().toISOString();
  req.body = JSON.stringify(body, null, 2);
  req.headers.set('Content-Type',
    'application/json');
}`,
  },
  {
    id: 'values',
    title: 'Pass values',
    note: "Shares a value with the scripts that run after this one in the same send (ctx.vars).",
    code: `// Script "Trace" (attached first)
function beforeRequest(req, ctx) {
  const id = 'req-' + Date.now();
  ctx.vars.set('traceId', id);
  req.headers.set('X-Trace-Id', id);
}

// Script "Log" (attached after it)
function afterResponse(res, ctx) {
  const id = ctx.vars.get('traceId');
  console.log('trace', id, '→', res.status);
}`,
  },
  {
    id: 'skip',
    title: 'Skip',
    note: "Stops the request from being sent when something's missing; the reason shows in its response.",
    code: `function beforeRequest(req, ctx) {
  if (!ctx.env.get('TOKEN')) {
    ctx.skip('TOKEN is missing');
  }
}`,
  },
  {
    id: 'debug',
    title: 'Debug',
    note: 'Prints what goes out and comes back. Output appears under Scripts in the response.',
    code: `function beforeRequest(req, ctx) {
  console.log(req.method, req.url);
  console.log('headers', req.headers.all());
}

function afterResponse(res, ctx) {
  console.log('status', res.status);
  console.log('took', res.durationMs, 'ms');
  console.info(res.json());
}`,
  },
];
