/**
 * The script API as the script editor's completions describe it: what
 * completes after each path ("req", "req.headers", "ctx.expect()"…). Keep in
 * sync with backend/scripting/javascript.go.
 */
export interface ScriptApiMember {
  label: string;
  type: 'property' | 'method';
  /** The signature, shown next to the name. */
  detail: string;
  /** One line, shown beside the list. */
  info: string;
}

const headersRead: ScriptApiMember[] = [
  { label: 'get', type: 'method', detail: '(name) → string | undefined', info: "A header's value (name in any case)." },
  { label: 'all', type: 'method', detail: '() → object', info: 'Every header as name → value.' },
];

export const SCRIPT_API: Record<string, ScriptApiMember[]> = {
  req: [
    { label: 'method', type: 'property', detail: 'string', info: 'GET, POST… Change it in beforeRequest.' },
    { label: 'url', type: 'property', detail: 'string', info: 'The URL; {{placeholders}} are filled in after the scripts.' },
    { label: 'body', type: 'property', detail: 'string', info: 'The request body.' },
    { label: 'headers', type: 'property', detail: 'Headers', info: 'get / set / remove / all' },
  ],
  'req.headers': [
    ...headersRead,
    { label: 'set', type: 'method', detail: '(name, value)', info: 'Adds a header, or replaces its value.' },
    { label: 'remove', type: 'method', detail: '(name)', info: 'Removes a header.' },
  ],
  res: [
    { label: 'status', type: 'property', detail: 'number', info: 'The HTTP status code.' },
    { label: 'body', type: 'property', detail: 'string', info: 'The response body as text.' },
    { label: 'json', type: 'method', detail: '() → any', info: 'The body parsed as JSON (throws if it is not JSON).' },
    { label: 'headers', type: 'property', detail: 'Headers', info: 'get / all' },
    { label: 'durationMs', type: 'property', detail: 'number', info: 'How long the request took.' },
  ],
  'res.headers': headersRead,
  ctx: [
    { label: 'env', type: 'property', detail: 'Env', info: "The selected environment's variables (read only)." },
    { label: 'vars', type: 'property', detail: 'Vars', info: 'Values shared with the scripts that run after this one.' },
    { label: 'test', type: 'method', detail: '(name, fn)', info: 'A test: passes unless fn throws or returns false.' },
    { label: 'expect', type: 'method', detail: '(value) → Expect', info: 'Assertions: toBe, toEqual, toContain…' },
    { label: 'skip', type: 'method', detail: '(reason)', info: "In beforeRequest: don't send the request." },
  ],
  'ctx.env': [
    { label: 'get', type: 'method', detail: '(key) → string | undefined', info: 'A variable of the selected environment.' },
  ],
  'ctx.vars': [
    { label: 'get', type: 'method', detail: '(key) → any', info: 'A value an earlier script set.' },
    { label: 'set', type: 'method', detail: '(key, value)', info: 'A value for the scripts that run after this one.' },
  ],
  'ctx.expect()': [
    { label: 'toBe', type: 'method', detail: '(expected)', info: 'Strictly equal (===).' },
    { label: 'toEqual', type: 'method', detail: '(expected)', info: 'Equal as JSON (objects and arrays too).' },
    { label: 'toBeTruthy', type: 'method', detail: '()', info: 'Truthy.' },
    { label: 'toBeFalsy', type: 'method', detail: '()', info: 'Falsy.' },
    { label: 'toContain', type: 'method', detail: '(item)', info: 'A string or array that includes item.' },
    { label: 'toBeGreaterThan', type: 'method', detail: '(n)', info: 'Greater than n.' },
    { label: 'toBeLessThan', type: 'method', detail: '(n)', info: 'Less than n.' },
  ],
  console: [
    { label: 'log', type: 'method', detail: '(...values)', info: "Shown under Scripts in the response." },
    { label: 'info', type: 'method', detail: '(...values)', info: 'Like log.' },
    { label: 'warn', type: 'method', detail: '(...values)', info: 'Shown as a warning.' },
    { label: 'error', type: 'method', detail: '(...values)', info: 'Shown as an error.' },
  ],
};

/** The names a script can use anywhere (its hook parameters and console). */
export const SCRIPT_GLOBALS: ScriptApiMember[] = [
  { label: 'req', type: 'property', detail: 'Request', info: 'In beforeRequest: the request, which you can change.' },
  { label: 'res', type: 'property', detail: 'Response', info: 'In afterResponse: the response.' },
  { label: 'ctx', type: 'property', detail: 'Context', info: 'env, vars, test, expect, skip' },
  { label: 'console', type: 'property', detail: 'Console', info: 'log, info, warn, error' },
];
