# Plan: Tags → User Scripts

Status: **in progress** · Branch: `refactor/tags-to-scripts` · Started 2026-11-19

Tags duplicate what favourites already do (mark and group requests). We turn them into
**user scripts**: a named piece of code the user writes and attaches to many requests.
Script execution is not designed yet. For now we only store the text and show a
"work in progress" notice with a donation link.

The work is split into small steps. Each step is **one commit** that the owner reviews
before the next step starts. Tick the box when a step is merged.

---

## Decisions

| # | Decision |
|---|----------|
| D1 | Existing tags are **deleted**. The migration drops tag data; it does not convert it. |
| D2 | Scripts behave like tags do today: they are listed in the left sidebar, clicking one lists the **requests attached to it** in the middle column, and the **Inspector** (right panel) shows the **script editor**. |
| D3 | Requests inside a script keep **manual ordering** (drag to reorder). |
| D4 | Donation link is a mock for now: `https://example.com/donate`. |
| D5 | The right panel is renamed **Inspector**. It is a container that shows a different component depending on what the user is doing (response, trash item, script). |
| D6 | Attaching works as today: the request "Tags…" dialog becomes "Scripts…", a request can have many scripts, and a script can be attached to many requests. |

### Assumptions (all confirmed 2026-11-19)

- **A1:** Per-request **colours** inside a script are kept, like favourites and collections (only ordering was confirmed).
- **A2:** In the Scripts view the Inspector **always** shows the selected script's editor, even when a request row is selected. To see a response, the user opens the request from another view.
  **Deleting** a request inside a script view **detaches** the script from it; the request
  itself is not moved to the trash (today, deleting inside a tag view trashes the request).
- **A3:** Script text is plain text with **JavaScript highlighting**, auto-saved after a short pause (like other editors in the app). Editing text is not part of the app's undo/redo history; CodeMirror's own undo covers it.
- **A4:** MCP tools are renamed (`add_tag` → `attach_script`, etc.). Any MCP client that calls the old names stops working. Acceptable before release.

---

## Target shape

```
┌─ sidebar rail ─┬─ group list ─────┬─ request list ──────────┬─ Inspector ───────────────┐
│ All            │ (collections /   │ requests of the active  │ shows one pane:           │
│ Recent         │  favourites /    │ group                   │  • message-pane           │
│ Collections    │  scripts)        │                         │  • response-pane          │
│ Favourites     │                  │                         │  • binned-request-pane    │
│ Scripts  ◄ new │                  │                         │  • script-pane (editor)   │
│ Trash          │                  │                         │                           │
└────────────────┴──────────────────┴─────────────────────────┴───────────────────────────┘
```

**One "active view" value** replaces the scattered flags in `WorkspaceStateService`
(`showingBin`, `showingAllRequests`, `showingFamousRequests`, `selectedTag`,
`selectedCollection`, `selectedFavouriteCollection`). It is a tagged union, roughly:

```ts
type ActiveView =
  | { kind: 'all' } | { kind: 'recent' } | { kind: 'bin' }
  | { kind: 'collection'; id: number }
  | { kind: 'favourite'; id: number }
  | { kind: 'script'; id: number };
```

The Inspector and the request list read this one value instead of combining flags.

### Inspector

The Inspector is a plain container that knows nothing about any view. Three pieces:

- **Content** (`inspector/inspector-content.ts`): a tagged union of what can be shown,
  e.g. `{ kind: 'response'; requestId }` or `{ kind: 'binned-request'; binId }`.
- **Panes** (`inspector/inspector-panes.ts`): one component per content kind, each taking
  that member as its required `content` input. The map is typed against the union, so a
  kind without a pane does not compile. Each pane owns its header (shared
  `inspector-header` component), data loading and actions.
- **Decision** (`core/services/inspector.service.ts`): the only code that turns workspace
  state into content. In A2 this moves into per-view rules.

Adding scripts means: add `{ kind: 'script'; scriptId }`, write `script-pane`, register it,
and add the rule. The Inspector itself does not change.

```
inspector/
  inspector.ts                    container: <ng-container *ngComponentOutlet>
  inspector-content.ts            content union
  inspector-panes.ts              kind → pane component
  inspector-header/               shared header
  panes/message-pane/             empty and multi-selection states
  panes/response-pane/            wraps response-viewer (also used by Zen mode)
  panes/binned-request-pane/      trash restore / delete permanently
core/services/inspector.service.ts   decides the content
```

**Out of scope for now:** splitting `requests-main-list` (2,250 lines) into a generic,
column-configurable grid. Scripts no longer need their own list (D2), so this can wait. It is
listed under "Later" so it isn't forgotten.

---

## Steps

### Stage A: refactor, no visible change

- [x] **A1. Inspector container.**
  Rename `request-panel` → `inspector`. Move the trash restore/delete card (and its
  "restore to collection" dialog) into `binned-request-detail`. The existing
  `response-viewer` already is the response detail, so it needs no wrapper. The Inspector
  decides which detail to show and keeps the header ("Response" for now; it becomes
  per-view in C3), the empty and multi-selection messages, and the response-loading effect.
  _Files:_ `frontend/src/app/request-panel/*` → `frontend/src/app/inspector/*`, new
  `binned-request-detail/`, `app.html`, `app.ts`.
  _Superseded by A1b: the container still had view-specific `@if` checks._

- [x] **A1b. Generic Inspector.**
  Make the Inspector a plain pane host (see "Inspector" above): content union, typed pane
  map, shared header, `InspectorService` decides the content. `binned-request-detail`
  becomes `panes/binned-request-pane`; new `message-pane` and `response-pane` (the
  response-loading effect moves into it). **Visible change:** pane titles are "Response"
  and "Deleted request"; message states have an empty header bar.
  _Files:_ `inspector/**`, `core/services/inspector.service.ts`.

- [x] **A2a. Active view.**
  Add `activeView` (`core/models/active-view.ts`) and `selectView()` to
  `WorkspaceStateService`. The six old flags become read-only values derived from it, so
  readers keep working; all 44 writers now call `selectView()`. `enterBinView` /
  `exitBinView` are gone. Project switch: all, recent and trash stay open, a collection,
  favourite or tag falls back to all (`selectViewForNewProject`). **Behaviour fixes:**
  switching project while in Trash no longer leaves the old project's trash card in the
  Inspector; deleting the last project from Recent or Trash leaves no view open.
  _Files:_ `core/models/active-view.ts`, `workspace-state.service.ts`, `request-groups.ts`,
  `app.ts`, `requests-main-list.ts`.

- [ ] **A2b. View definitions.**
  Add one view definition per view kind (all,
  recent, bin, collection, favourite, tag) holding what is specific to that view: its
  capabilities (orderable, colourable, today in `request-view-capabilities.ts`) and its
  Inspector rule `inspector(selection) → InspectorContent`. `InspectorService` then just
  calls the active view's rule, plus the shared multi-selection rule.
  _Files:_ `core/services/inspector.service.ts`, new `core/models/view-definitions.ts`,
  `request-view-capabilities.ts`.

- [ ] **A3. Request list reads active view.**
  Move `requests-main-list` (`activeViewKind`, `activeGroupKey`, loading) onto `activeView`,
  then remove the old flags that nothing reads any more.
  _Files:_ `requests-main-list/*`, `request-view-capabilities.ts`,
  `workspace-state.service.ts`.

### Stage B: backend tags → scripts

- [ ] **B1. Database migration.**
  Drop `request_tags`, `tag_appearances` and `tags` (D1). Create `scripts` (`id`,
  `project_id`, `name`, `content`, timestamps), `request_scripts` (with `user_sort_order`
  and `user_color`, like `request_tags`) and `script_appearances`.
  _Files:_ `backend/database/db.go`, `backend/database/db_test.go`.

- [ ] **B2. Go commands and models.**
  Rename `backend/commands/tags` → `backend/commands/scripts` (add, remove, rename, delete,
  list, list_for_requests, list_requests, reorder, set_color, set_appearance) and add
  `get_content` / `set_content`. Regenerate the Wails bindings.
  _Files:_ `backend/commands/scripts/*`, `backend/models/*`, `frontend/bindings` (generated).

- [ ] **B3. MCP tools.**
  Rename the tag tools (A4): `list_scripts`, `list_script_requests`, `attach_script`,
  `detach_script`, `rename_script`, `reorder_script_requests`, `set_script_request_colors`.
  Content editing over MCP is left out until execution is designed.
  _Files:_ `backend/mcp/catalog.go`, `tools_organize.go`, `tools_read.go`, tests, `docs/mcp.md`.

> Stage B breaks the frontend's tag calls. B2 includes the minimum frontend edits
> (`tag.service.ts` → `script.service.ts`, renamed calls) so every commit still builds.

### Stage C: scripts UI

- [ ] **C1. Sidebar and naming.**
  The rail entry "Tags" → "Scripts" with a code icon (`{ }`). The group list, context menu
  ("Rename Script", "Delete Script") and "Add new script" dialog use the new names. View
  ids `tags`/`tag` → `scripts`/`script`.
  _Files:_ `sidebar-rail/sidebar-views.ts`, `request-groups/*`, `request-group-list/*`,
  `request-view-capabilities.ts`.

- [ ] **C2. Attach dialog.**
  The request "Tags…" dialog and the bulk tag dialog become "Scripts…": pick from existing
  scripts, or create one by name. Deleting requests while a script is open detaches the
  script instead of trashing the requests (A2); the confirm text says so.
  _Files:_ `requests-main-list/*`.

- [ ] **C3. Script editor in the Inspector.**
  New `script-pane` (content `{ kind: 'script'; scriptId }`): script name heading, CodeMirror editor (JavaScript mode, A3) with
  auto-save, and a WIP banner: "Scripts can't run yet. This feature is in progress, so if
  you'd like to see it sooner, consider supporting development." with a link to D4. The
  script view's Inspector rule always returns it (A2).
  _Files:_ new `inspector/panes/script-pane/`, `inspector/inspector-content.ts`,
  `inspector/inspector-panes.ts`, view definitions, `core/services/script.service.ts`.

### Later (not scheduled)

- Generic column-configurable `data-grid`, then move `requests-main-list` onto it piece
  by piece (virtual scroll, selection, sort, column resize, reorder, drag-out, colours).
- Script execution.

---

## Manual sanity tests

The repo doesn't use frontend tests (see `AGENTS.md`), so check these by hand in
`wails3 dev` after each step. Every step must also pass `go build ./...`, `go test ./...`
and the frontend build.

### Smoke test: after every step

1. App starts with no errors in the dev tools console.
2. Switch project and environment; the lists reload.
3. **All requests:** select a request, edit its URL and send it; the response appears in
   the right panel.
4. **Recent:** the request you just sent is at the top.
5. **Collections:** open a collection, reorder two requests, colour one; restart the
   app and both changes are still there.
6. **Favourites:** add a request to a favourite and open it; it's listed.
7. **Trash:** delete a request, open Trash, select it and restore it; it's back in its
   collection. Delete one permanently.
8. Multi-select (shift-click) several requests; the right panel says you are in multi-selection mode.
9. Drag a request onto a collection and onto a favourite in the left list.
10. Undo (Ctrl+Z) the last organise action.

### Extra checks per step

| Step | Also check |
|------|------------|
| A1, A1b | Right panel shows the right thing in every view: response heading and count, trash card with "Deleted request" title, "restore to collection" dialog when the original collection is gone, and the empty-state message. |
| A1b | Switch quickly between a request, Trash, a collection and multi-selection: never a stale pane, response list or trash card. Select a request, then another: responses reload for the new one. Send a request: the new response appears. Open Zen mode on a request: responses still show. |
| A2a, A2b | Clicking each rail entry and each group shows the right list and the right Inspector content. Switching quickly between Trash and a collection never leaves a stale trash card or response. |
| A2a | Start the app: it opens on All requests. Switch project from All, Recent and Trash: you stay there (Trash shows the new project's items, no old trash card). Switch project from a collection: you land in All. Open the Collections folder while in All: the list empties; while in a collection: the collection stays. Rename the open tag: you stay in it. Delete the open collection, favourite and tag: the list empties and the Inspector says "Select a request…". |
| A3 | Sorting, reorder handles and colour controls appear only in collections, favourites and tags. Scroll position and selection are remembered per group. |
| B1 | Start the app on a copy of an existing database: it starts, tags are gone, all other data is intact. Starting it a second time doesn't fail. |
| B2 | Create, rename and delete a script; attach it to and detach it from a request; reorder and colour requests inside a script. |
| B3 | From an MCP client: the tool list shows the new names, and attaching or reordering needs approval as before. |
| C1 | The Scripts rail entry has its icon on Windows **and macOS** (icons need explicit size, see `AGENTS.md`). Rename and delete work from the context menu. |
| C2 | Attach two scripts to one request; bulk-attach a script to several selected requests; creating a script from the dialog adds it to the sidebar. Inside a script, delete one request and then several: they leave the script, are **not** in Trash, and are still in their collection. |
| C3 | Select a script: the editor shows its text. Type, wait, switch away and back: the text is saved. Restart: still saved. The WIP banner and donate link show. Selecting a request inside the script view keeps the editor (A2). |
