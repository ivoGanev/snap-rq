# Scripting: design notes

Written 2026-11-20 as a design record before the work starts. Phases 1 and 2 were built
on 2026-11-21 (see "Status" at the end).
Scripts exist today as named code attached to requests (see `tags-to-scripts.md`); this
document is about **running** them.

## Core idea

Scripts run in the **backend**, as part of `request.send`, in a **sandboxed JavaScript
engine** ([goja](https://github.com/dop251/goja), pure Go), in a **fixed order per request**.
`request.send` already fills in `{{variables}}` and decrypts secret variables at that point,
so the pipeline slots in there.

## The pipeline for one send

```
request R with scripts [Auth, Sign, Assert] (in R's order)
  ├─ beforeRequest:  Auth → Sign → Assert     each sees the request as the previous left it
  ├─ fill in {{variables}} and secrets
  ├─ send HTTP
  ├─ afterResponse:  Auth → Sign → Assert     read the response, run tests, extract values
  └─ save the response + the run log (console output, tests, errors, durations)
```

A script defines either hook, or both. goja doesn't support `export` (ES modules), so a
script just **defines functions with these names** and the backend looks them up:

```js
// script "Auth"
function beforeRequest(req, ctx) {
  req.headers.set("Authorization", "Bearer " + ctx.env.get("TOKEN"));
  console.log("signed for", req.url);
}

function afterResponse(res, ctx) {
  ctx.test("status is 200", () => res.status === 200);
  ctx.env.set("TOKEN", res.json().access_token); // e.g. on a login request
}
```

A script without a `beforeRequest` is skipped in that phase, and likewise for `afterResponse`.

## Why the backend, not the WebView

- A script in the WebView could call **any** app command through the Wails bindings, read
  every secret and delete data. A goja runtime starts with plain JavaScript only: no files,
  no network, no `require`, no app. Scripts get exactly what we `Set` ("allow, not deny").
- Secrets stay in the backend: `ctx.env.get("TOKEN")` reads values the backend decrypted. They
  never reach the UI; the run log masks secret values like MCP output does.

## How goja works for us (sketch)

```go
// RunBefore runs one script's beforeRequest hook on req, if it has one.
func RunBefore(source string, req *Request, env map[string]string, log *[]string) error {
	vm := goja.New()

	// Time limit: stop endless loops.
	timer := time.AfterFunc(2*time.Second, func() { vm.Interrupt("script timed out after 2s") })
	defer timer.Stop()

	// console.log → the run log.
	console := vm.NewObject()
	console.Set("log", func(args ...goja.Value) { *log = append(*log, joinArgs(args)) })
	vm.Set("console", console)

	// 1. Load the script: defines its functions, runs nothing yet.
	if _, err := vm.RunString(source); err != nil {
		return fmt.Errorf("script error: %w", err) // includes line and column
	}
	// 2. Skip scripts without this hook.
	hook, ok := goja.AssertFunction(vm.Get("beforeRequest"))
	if !ok {
		return nil
	}
	// 3. Expose only what we choose.
	jsReq := vm.NewObject()
	jsReq.Set("method", req.Method)
	jsReq.Set("url", req.URL)
	jsReq.Set("body", req.Body)
	headers := vm.NewObject()
	headers.Set("get", func(name string) string { return req.Headers[name] })
	headers.Set("set", func(name, value string) { req.Headers[name] = value })
	jsReq.Set("headers", headers)
	ctx := vm.NewObject()
	ctxEnv := vm.NewObject()
	ctxEnv.Set("get", func(key string) string { return env[key] })
	ctx.Set("env", ctxEnv)

	// 4. Call it; a throw or the timeout comes back as err.
	if _, err := hook(goja.Undefined(), jsReq, ctx); err != nil {
		return err
	}
	// 5. Read back what it may have changed.
	req.Method = jsReq.Get("method").String()
	req.URL = jsReq.Get("url").String()
	req.Body = jsReq.Get("body").String()
	return nil
}
```

In `request.send`, before interpolation:

```go
for _, s := range scriptsFor(req.ID) { // run order, enabled only
	if err := scripting.RunBefore(s.Content, &scriptReq, values, &log); err != nil {
		obs.Finish(nil, fmt.Errorf("script %q stopped the request: %w", s.Name, err))
		return // not sent
	}
}
// … interpolate, send, the same loop with RunAfter(res) …, save response + log
```

Properties:

- **Sandbox**: only what we `Set` exists.
- **Endless loops**: `vm.Interrupt` from a timer stops them.
- **Errors**: normal Go errors with line and column ("Auth, line 3: TOKEN is undefined").
- **Isolation**: a fresh `goja.New()` per run; compiled scripts can be cached by content
  (`goja.Compile`) if start-up cost ever matters.
- **Concurrency**: each send already runs in its own goroutine with its own runtime; a goja
  runtime is never shared between goroutines.

The real code wraps this in a small `Runtime` so both hooks share one setup.

## Language-neutral from the start

Only the engine that runs one hook is language-specific:

```go
type Engine interface {
	BeforeRequest(source string, req *Request, ctx *Context) error
	AfterResponse(source string, res *Response, ctx *Context) error
}
```

Scripts get a `language` field. `req`, `res` and `ctx` are the same data for every language.

Other options considered:

| Option | Notes |
|---|---|
| **JavaScript, goja** (chosen to start) | What API users know; Postman, Insomnia and Bruno use JS, so scripts and know-how carry over. Pure Go. |
| Lua (gopher-lua) | Tiny, fast, easy to sandbox; few web developers write it. Cheap second language. |
| Starlark (`go.starlark.net`) | Python syntax, built for safe embedded scripting; not real Python (no imports, no pip). Cheap second language. |
| Go (yaegi) | Interprets Go; harder to sandbox; niche. |
| Real Python | gpython is incomplete. Possible later via WebAssembly (CPython on wazero, ~25 MB, tens of ms to start). |
| WebAssembly (wazero / Extism) | Any compiled language in a strong sandbox, but users would compile scripts first: doesn't fit "type in the editor". |
| Installed runtimes (node, python, deno) | Real ecosystems, but they must be installed, start slowly, and run with **full machine access**: a shared malicious script could read files and keys. Only Deno (deny-by-default permissions) would be acceptable, as an opt-in advanced mode. |

## Script API (first version)

| `ctx.…` / argument | What it does |
|---|---|
| `req` | method, url, headers, body; changeable in `beforeRequest` |
| `res` | status, headers, body, `json()`, time taken; read-only |
| `ctx.env.get/set` | environment variables. Writes apply **after** the run, together, through the normal variable commands (undo, change events, secret encryption keep working) |
| `ctx.vars` | values passed between the scripts of one run |
| `ctx.test(name, fn)`, `ctx.expect(x)` | assertions, shown pass/fail |
| `console.log` | captured into the run log |
| `ctx.crypto` | sha256, hmac, base64, uuid (request signing), native Go |
| `ctx.skip(reason)` | in `beforeRequest`: don't send |

Later: calling other requests from a script (e.g. log in first). Powerful, but the hardest part
to make safe (secrets, endless chains).

## Errors

- Error in `beforeRequest`: the run stops and the request is **not sent**; the error names the
  script and line.
- Error in `afterResponse`: recorded as a failed test; the response is still saved and the
  other scripts still run.
- A timeout counts as an error in that script.

## Data changes

- `request_scripts` gets `run_order` and `enabled`. Note: its `user_sort_order` orders requests
  **inside a script's Attachments list**; `run_order` is the order scripts **run on a request**.
- `responses` gets `script_log` (JSON): output, test results, errors and durations per script,
  kept with the response.

## UI

- Request editor: the script chips become a small ordered list: drag to reorder, switch each on
  or off for that request.
- Response viewer: a **Scripts** tab with tests (✓/✗), console output and errors per script.
- Script editor: "Test against request…" runs the hooks against a chosen request without
  sending.
- The WIP banner goes once phase 2 ships.

## What Postman offers, and what we take

Postman's scripts run pre-request and post-response at collection, folder and request level
(collection → folder → request), through the `pm` API: change the request, read the response,
`pm.test`/`pm.expect` (Chai), variables at several scopes (environment, collection, globals,
iteration data), `pm.sendRequest`, cookies, flow control in the Collection Runner
(`setNextRequest`), `pm.visualizer` (HTML), console, built-in libraries (lodash, moment,
crypto-js, uuid, cheerio, xml2js, Ajv, chai) and async/await. Most common uses: auth tokens,
tests (often in CI via Newman), passing values between requests, request signing, test data.

| Postman | SnapRQ |
|---|---|
| Pre-request / post-response | `beforeRequest` / `afterResponse` (phases 1–2) |
| Collection / folder / request levels | One script attached to many requests, in a set order per request; collection-level attachment possible later |
| `pm.request`, `pm.response`, console | Phases 1–2 |
| `pm.test` / `pm.expect` | Phase 2, a small `expect` (Chai is big) |
| Environment get/set | Phase 4, through the variable commands |
| crypto, uuid, base64 | Phase 4, `ctx.crypto` |
| `pm.sendRequest` | Later |
| Cookies | Later, read-only first |
| Collection Runner / `setNextRequest` / CLI | A separate "runner" feature; a Pro / CI candidate |
| Visualizer | Skipped: rendering HTML in the app is the XSS risk we avoid |
| `pm.require`, npm, lodash/moment | Skipped at first |
| Async / promises | goja supports promises; hooks stay synchronous at first |

## Phases

1. **Engine and `beforeRequest`**: goja sandbox, request changes, `console.log`, time limits,
   run log saved with the response, `run_order` / `enabled`.
2. **`afterResponse` and tests**: `res`, `ctx.test` / `expect`, the Scripts tab.
3. **Ordering UI**: drag to order and toggle per request.
4. **Environment writes and crypto**: `ctx.env.set` through the commands, `ctx.crypto`.
5. **Later**: calling other requests, "test against request", MCP access to run logs, a
   collection runner.

## Decisions

1. **Script shape**: one script with two optional hooks, `beforeRequest` and `afterResponse`.
2. **`afterResponse` order**: the same as `beforeRequest` (as Postman).
3. **Engine**: goja, pinned to a version that still builds with Go 1.25.
4. **Secret variables**: scripts read them (`ctx.env.get`); writing variables is phase 4.

## Status

Built (phases 1 and 2):

- `backend/scripting`: the `Engine` interface, the goja engine (`javascript.go`) and a `Session`
  that runs one send's scripts. Each script gets its own runtime for the whole send; loading and
  each hook are limited to 5 seconds.
- `request.send` runs the enabled scripts with code, in `run_order`: `beforeRequest`, then
  variables and secrets, the HTTP call, then `afterResponse` (not when no response arrived).
- A `beforeRequest` error or `ctx.skip(reason)` stops the send; it is saved like a failed request
  (status 0, the reason as the message), with the scripts' log.
- `responses.script_log` keeps the log; secret values a script read or the request used are
  masked in it. `request_scripts` has `run_order` (attach order) and `enabled`.
- The response view shows a collapsible **Scripts** section (tests, console, errors per script and
  phase), also under a failed request's message. The script views' Inspector is a quick
  reference.

- The script view's Inspector is **Try it**: pick a request, run `beforeRequest` (shows the request
  it would send, variables filled in, secret values masked) or `afterResponse` (against the request's
  last saved response) on the editor's current code, without sending (`script.try`); or send for
  real and see that send's script log. The reference folds out below.
- The script editor completes the API (`script-api.ts`) and underlines syntax errors from goja
  (`script.check`).

Not yet: ordering and switching scripts per request in the UI (phase 3), `ctx.env.set` and
`ctx.crypto` (phase 4), and everything in phase 5. Script logs are not kept when a request is
restored from the trash bin.
