# Tests

## End-to-end tests

These run the real app (frontend and Go backend) in a browser: `wails3 task test:e2e`, and on every pull request.
Each comes with a way to check the same thing by hand. The steps use `https://httpbin.org`, which answers any status code or delay.

1. **Fast failures never stay in flight**
   `send-lifecycle.e2e.ts`. Send a request to `http://%zz` many times quickly. Each send ends with an error, and the request never stays on "Sending…".

2. **A long send reports progress and finishes once**
   `send-lifecycle.e2e.ts`. Send `https://httpbin.org/delay/2`. The in-flight view's clock counts up, then the response replaces it, and nothing changes after that.

3. **HTTP error statuses are ordinary responses**
   `send-lifecycle.e2e.ts`. Send `https://httpbin.org/status/404`, then `/status/500`. Each shows its status code as a response, not as a failed send.

4. **A request stays in flight until its last send finishes**
   `send-lifecycle.e2e.ts`. Send `https://httpbin.org/delay/3`, then change the URL to `http://%zz` and send again. The second send fails at once, but the request stays in flight until the first one answers.

5. **A failed send explains what went wrong, not status 0**
   `request-error.e2e.ts`, with `backend/commands/requests/send_errors_test.go` for each kind of failure. Add the header `{"Accept": "application/json"}` (a JSON object where a header belongs) and send. The inspector shows "Couldn't send the request", then "Header line 1 isn't a header" and how headers are written, and "Error" in the history. There's no status line and no "No headers". Then send to `http://127.0.0.1:1/`: it reads "Nothing is answering at 127.0.0.1:1", and Go's own error is folded under Details.

6. **The theme picker closes on an outside click**
   `theme-picker.e2e.ts`. Open the theme picker in the footer and click in the middle of the window. It closes.

7. **Script results show in the inspector, with secrets masked**
   `scripts-and-secrets.e2e.ts`. Create an environment with a secret variable `TOKEN`, and send a request to `https://httpbin.org/headers` with the header `Authorization: Bearer {{TOKEN}}`. Attach this script:
   ```js
   function afterResponse(res, ctx) {
     console.log("token is " + ctx.env.get("TOKEN"));
     ctx.test("server got the token", () =>
       ctx.expect(res.json().headers.Authorization).toBe("Bearer " + ctx.env.get("TOKEN")));
     ctx.test("is a teapot", () => ctx.expect(res.status).toBe(418));
   }
   ```
   The Scripts section shows 1 passed and 1 failed, with "is a teapot" marked failed, and the console reads `token is •••••`.

8. **Dropdowns open the app's own list**
   `select.e2e.ts`. With two or three environments, open the Environment dropdown: the list is themed like the app on every platform, not the system's menu. Pick with the mouse; then with the arrow keys and Enter; Esc and a click outside close it unchanged; with the list closed, typing a letter jumps to the next environment starting with it.

9. **A JSON body is drawn in the theme's syntax colours**
   `body-json-colours.e2e.ts`. Give a request the JSON body `{"name": "Ada", "age": 36, "admin": true, "team": null}` and open its Body tab, in the drawer and in Zen mode. Keys, strings, numbers, `true` and `null` each have their own colour, the same as in a JSON response, in every theme.

10. **The top bar is the window's title bar**
   `top-bar.e2e.ts` and `top-bar-webkit.e2e.ts`, in Chromium and WebKit. On macOS the bar sits at the very top, with the traffic lights centred on it and the sidebar toggle just right of them. On Windows it sits at the very top with minimise, maximise and close at its right end. Drag the empty bar to move the window; double-click it to maximise.

11. **Every variable name that can be saved shows as a chip**
   `variable-chips.e2e.ts` and `variable-chips-webkit.e2e.ts`, in Chromium and WebKit. Add the variable `fleet-management.v2` to an environment and select it. Use `{{fleet-management.v2}}` in a request's URL, a header and the body: each shows as a chip, not plain text. In the variables editor, a name with a space or any symbol other than `_`, `-` and `.` is underlined in red with an explanation, and isn't saved. The allowed names are listed in `testdata/variable-names.json`, which the Go and frontend unit tests share.

12. **A paw marks the last sent request and its collection**
   `last-used-paw.e2e.ts` and `last-used-paw-webkit.e2e.ts`, in Chromium and WebKit. In a new collection, create three requests and send the first two. Under Collections, only that collection has the paw, at its right end. Open it: only the request sent last has the paw, at the right end of its row. Send another request: both paws move to it straight away, without a reload.

13. **Secrets in a response stay hidden until revealed**
   `response-secrets.e2e.ts` and `response-secrets-webkit.e2e.ts`, in Chromium and WebKit, against the stub's `/secrets` (the mock server's `/mock/secrets` for manual runs). Send a request with `?api_key=key-url-6` in its URL, whose response has a password and tokens in the body, and an auth token and session cookie in the headers. The URL line, body, headers and Cookies table show `•••••` for them, and ordinary fields as sent. Each eye reveals only its own line or section; opening another response hides everything again. A response with nothing secret shows a copy button but no eye. Copy takes what is shown: `•••••` while hidden, the real value once revealed. What counts as secret is the MCP masking list plus the names added in Settings → MCP (`backend/mcp/display_test.go`).

14. **The trash bin asks before deleting and restores to the right place**
   `trash-bin.e2e.ts`, in Chromium. Move a few requests to the bin and open Trash bin.
   - Select one: Delete permanently in the inspector asks first, and Cancel keeps it. Select two: the pane lists them, Delete permanently names "2 requests", and confirming removes both.
   - Empty bin in the top bar asks first; Cancel keeps everything, and confirming empties the bin and hides the button.
   - Restore reads "Restore to *collection*" and puts the request back there. When its collection was deleted it reads "Restore…" and opens the picker. Restore all on a selection where one collection is gone restores nothing until a collection is picked.

15. **Clearing a request's history asks first and keeps its usage**
   `clear-history.e2e.ts`, in Chromium, with `backend/commands/responses/clear_test.go`. Send a request to a failing address twice and open it from Failing. The bin at the right of the History bar asks first ("All 2 saved responses"), and Cancel keeps them. Confirming shows "No responses for this request." and drops the request from Failing; its status resets, while its usage (Recent, the usage meter, the paw) stays. Clicking the bin doesn't fold or unfold the timeline, and Zen mode's History bar has no bin.

16. **Only the body scrolls, and revealing a secret keeps its place**
   `response-layout.e2e.ts` and `response-layout-webkit.e2e.ts`, in Chromium and WebKit, against the stub's `/long`: a JSON list of 300 items, one with a very long note, and an `access_token` near item 200. Send it and open the response. Scroll the body down and to the right: the status line, URL and section headings don't move, the headers don't scroll sideways, and the body's horizontal scrollbar sits at the bottom of the panel. Scroll down to the hidden `access_token` and reveal the body: the token appears in place, and the body stays scrolled where it was, with the same line numbers.

## Tests worth writing

Add a line when you find something a test should cover: what should happen, and why it matters. Remove it once the test exists, and add the test above if it's end-to-end.

1. **Deleting a collection keeps its requests recoverable.** Today they're destroyed outright: they don't go to the trash bin, and Undo can't bring them back. Needs a decision first.
2. **The in-flight view's Cancel button.** The view appears after 200 ms, the clock ticks, and Cancel returns to the previous response with nothing saved. Go tests cover cancelling, but not the button or the view.
3. **Undo and redo from the keyboard.** Ctrl+Z after deleting a request brings it back in the list, and Ctrl+Y removes it again.
4. **Importing a cURL command or an OpenAPI file through the UI.** The parsers have Go tests; the import dialogs have none.
5. **Rendering in WebKit.** Only the top bar's, the variable chips', the last-used paw's, the response secrets' and the response layout's tests run in WebKit; the rest use Chromium, but macOS and Linux show the app in WebKit (see the SVG sizing rule in AGENTS.md).
6. **Restoring a locked vault with the recovery key.** After the key is lost, secrets show as locked, and entering the recovery key unlocks them.
7. **The MCP server with a real client.** A client connects, lists the tools, and needs approval before running a command that writes.
