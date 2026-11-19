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

### Views

The workspace shows one **view** at a time: all requests, recent, the trash bin, or the
requests of one collection, favourite list or tag (script, after Stage C). Each view is a
small object in `core/workspace/workspace-view.ts` that holds everything specific to it:

```ts
class TrashView {
  readonly kind = 'bin';
  readonly title = 'Trash bin';            // list header
  readonly canReorder = false;             // rows can be drag-reordered
  readonly canColour = false;              // rows can be coloured
  readonly survivesProjectSwitch = true;   // stays open on project switch
  inspect(selection) { … }                 // what the Inspector shows
}
```

`WorkspaceStateService.view` holds the open view (or `null`); switch with
`selectView(new TrashView())`. Readers ask the view (`view()?.canReorder`,
`view()?.title`) instead of combining flags. Adding a view means adding one class.

### Inspector

The Inspector (`inspector/inspector.ts`) is a plain container that knows nothing about any
view. The same file holds the `InspectorContent` union (what can be shown, e.g.
`{ kind: 'response'; requestId }`) and the map from each kind to its pane component, typed
so a kind without a pane does not compile. It shows the multi-selection message, otherwise
whatever `view.inspect(selection)` returns. Each pane owns its header (shared
`inspector-header`), data loading and actions.

Adding scripts means: add `{ kind: 'script'; scriptId }`, write `script-pane`, add it to the
map, and have `ScriptView.inspect()` return it.

```
core/workspace/workspace-view.ts   the views (one class each)
inspector/
  inspector.ts                     content union + pane map + container
  inspector-header/                shared header
  panes/message-pane/              empty and multi-selection states
  panes/response-pane/             wraps response-viewer (also used by Zen mode)
  panes/binned-request-pane/       trash restore / delete permanently
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

- [x] **A2b. View definitions.**
  Add one view definition per view kind (all,
  recent, bin, collection, favourite, tag) holding what is specific to that view: its
  capabilities (orderable, colourable, today in `request-view-capabilities.ts`) and its
  Inspector rule `inspector(selection) → InspectorContent`. `InspectorService` then just
  calls the active view's rule, plus the shared multi-selection rule.
  _Files:_ `core/services/inspector.service.ts`, new `core/models/view-definitions.ts`,
  `request-view-capabilities.ts`.
  _Superseded by A2c: the tables split one view across several files._

- [x] **A2c. One coherent view domain.**
  Replace `active-view.ts`, `view-definitions.ts` and `request-view-capabilities.ts` with
  one class per view in `core/workspace/workspace-view.ts` (see "Views" above). Fold the
  Inspector content union, pane map and `InspectorService` into `inspector/inspector.ts`.
  `state.activeView` becomes `state.view` (null when nothing is open). The request list
  reads `view.canReorder`, `view.canColour` and `view.title`. No visible change.
  _Files:_ `core/workspace/workspace-view.ts`, `inspector/**`, `workspace-state.service.ts`,
  `request-groups.ts`, `app.ts`, `requests-main-list.ts`.

- ~~**A3. Request list reads the view.**~~ Moved to "Later" (2026-11-19): scripts don't
  need it, the old flags are read-only so they can't drift, and it only touches the riskiest
  file.

### Stage B: backend tags → scripts

- [x] **B1. Script tables (nothing removed).**
  New idempotent step `createScriptTables`: `scripts` (`id`, `project_id`, `name`,
  `content`; name unique per project), `request_scripts` (`user_sort_order`, `user_color`,
  like `request_tags`) and `script_appearances`, all cascading from the project. No
  timestamps until something needs them. The tag tables stay until B2, so every commit
  leaves a working app.
  _Files:_ `backend/database/db.go`, `backend/database/db_test.go`.

- [x] **B2. Go commands and models.**
  `backend/commands/tags` → `backend/commands/scripts`. Command ids: `script.attach`
  (was `tag.add`; creates the script if new), `script.detach` (was `tag.remove`),
  `script.list`, `script.list_for_requests`, `script.list_requests`, `script.rename`,
  `script.delete`, `script.reorder`, `script.set_color`, `script.set_appearance`, plus new
  `script.get` (with content, read-only) and `script.set_content` (not undoable, like
  `request.update`). Commands run by id through one runner, so there are no Wails bindings to
  regenerate. **Changed from tags:** names keep their case (only trimmed); rename changes the
  name in place (id, content and links stay) and refuses a name already in use instead of
  merging; a new script gets its default sidebar icon at once; delete's undo restores the
  content. **Bugs fixed on the way:** undoing an attach removed that tag from every request;
  detach looked the name up across all projects. Tests in `commands/scripts/scripts_test.go`.
  Frontend: only `tag.service.ts` changes (command ids, `script_name`, `script_id`, `script`
  change events); C1 renames the rest.
  Also moved the other code that touches tags onto scripts: request delete (and its undo
  snapshot), request duplicate, trash restore and icon cleanup, and the startup step
  `dropTagTables` drops the tag tables
  (`DROP TABLE IF EXISTS request_tags, tag_appearances, tags`, D1) and remove undo entries
  for tag commands (`DELETE FROM undo_log WHERE command LIKE 'tag.%'`). Old undo and
  trash snapshots keep their `tag_links` / `tags` fields, which are then ignored.
  _Files:_ `backend/commands/scripts/*`, `backend/commands/{requests,bin,icons}/*`,
  `backend/models/*`, `backend/database/db.go`, `frontend/.../core/services/tag.service.ts`.

- [x] **B3. MCP tools.**
  Renamed the tag tools (A4): `list_tags` → `list_scripts`, `list_tagged_requests` →
  `list_script_requests`, `add_tag` → `attach_script`, `remove_tag` → `detach_script`,
  `rename_tag` → `rename_script`, `reorder_tagged_requests` → `reorder_script_requests`,
  `set_tag_request_colors` → `set_script_request_colors`; inputs take `script_name`.
  Content editing over MCP is left out until execution is designed.
  _Files:_ `backend/mcp/catalog.go`, `tools_organize.go`, `tools_read.go`, tests, `docs/mcp.md`.

> Stage B breaks the frontend's tag calls. B2 includes the minimum frontend edits
> (`tag.service.ts` → `script.service.ts`, renamed calls) so every commit still builds.

### Stage C: scripts UI

- [x] **C1. Sidebar and naming.**
  The rail entry "Tags" → "Scripts" with a code icon (`{ }`). The group list, context menu
  ("Rename Script", "Delete Script") and "Add new script" dialog use the new names. View
  ids `tags`/`tag` → `scripts`/`script`.
  _Done:_ `tag.service.ts` → `script.service.ts` (`ScriptApiService`, `attachScript`/`detachScript`),
  `TagView` → `ScriptView`, `selectedTag` → `selectedScript`, sidebar folder and theme icon
  feature `tags` → `scripts`, `tag-*` CSS classes → `script-*`, UI text says "Attach script".
  Behaviour unchanged.

- [x] **C2. Attach dialog.**
  The request "Tags…" dialog and the bulk tag dialog become "Scripts…": pick from existing
  scripts, or create one by name. Deleting requests while a script is open detaches the
  script instead of trashing the requests (A2); the confirm text says so.
  _Done:_ the dialogs already picked an existing script or created one by name; the context
  menu entry is now "Scripts…". Delete (key or menu) inside a script view detaches, and the
  menu item reads "Detach from script". Re-attaching no longer duplicates the chip.

- [x] **C3. Script editor in the Inspector.**
  New `script-pane` (content `{ kind: 'script'; scriptId }`): script name heading, CodeMirror editor (JavaScript mode, A3) with
  auto-save, and a WIP banner: "Scripts can't run yet. This feature is in progress, so if
  you'd like to see it sooner, consider supporting development." with a link to D4. The
  script view's Inspector rule always returns it (A2).
  _Files:_ new `inspector/panes/script-pane/`, `inspector/inspector-content.ts`,
  `inspector/inspector-panes.ts`, view definitions, `core/services/script.service.ts`.
  _Done:_ `inspector/panes/script-pane/` reuses `CodeEditor`, which gained a `javascript`
  language (`@codemirror/lang-javascript`, coloured from the `--syntax-*` palette). Saves
  500 ms after typing stops and when switching script or view. `ScriptView` now holds the
  `Script` (for its id) and its `inspect()` always returns the script. The donate link opens
  in the system browser.

### Stage D: focused script view (2026-11-19, replaces parts of C2/C3)

After trying C3 the request list inside a script view felt wrong. A script view now has no
request list: the middle column is the script editor, the Inspector shows the WIP message
and the requests the script is attached to. Scripts are attached from other views only
(request editor chips, "Scripts…" menu, drag onto the sidebar).

- [x] **D1. The view picks the middle column.** Every view has `main: 'requests' | 'script'`;
  `app.html` switches on it. Entering a script view drops the open request and row selection.
- [x] **D2. Editor in the middle, info on the right.** New `script-editor/` (moved from the C3
  Inspector pane: name heading, JS editor, autosave, "Not saved"). New Inspector content
  `{ kind: 'script-info' }` → `ScriptInfoPane`: WIP banner with donate link, and
  "Attached to N requests" (reloads after every command).
- [ ] **D3. Drop the script branch of the request list.** `scriptRequests`, reorder/colour in a
  script, C2's "Detach from script" delete; `ScriptView` loses `canReorder`/`canColour`.
- [ ] **D4. Backend clean-up (later, by decision).** `script.reorder`, `script.set_color`, their
  MCP tools and the `user_sort_order`/`user_color` columns of `request_scripts` are unused by
  the UI. Kept for now.

### Later (not scheduled)

- **A3. Request list reads the view.** Move the rest of `requests-main-list`
  (`activeViewKind` branches, `activeGroupKey`, `activeRequests`, `loadActiveGroup`'s seven
  view parameters) onto `state.view` with `switch (view.kind)`, add `groupKey()` to the
  views, then delete the six read-only flags in `WorkspaceStateService`.
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
| A2a–A2c | Clicking each rail entry and each group shows the right list and the right Inspector content. Switching quickly between Trash and a collection never leaves a stale trash card or response. |
| A2a | Start the app: it opens on All requests. Switch project from All, Recent and Trash: you stay there (Trash shows the new project's items, no old trash card). Switch project from a collection: you land in All. Open the Collections folder while in All: the list empties; while in a collection: the collection stays. Rename the open tag: you stay in it. Delete the open collection, favourite and tag: the list empties and the Inspector says "Select a request…". |
| A3 | Sorting, reorder handles and colour controls appear only in collections, favourites and tags. Scroll position and selection are remembered per group. |
| B1 | Start the app on a copy of your real database: it starts, and tags, collections, favourites and Trash all still work. Start it again: no errors. `go test ./backend/database/ -run TestCreateScriptTables` passes. |
| B2 | Start on a copy of a real database: tags are gone, everything else is intact, Ctrl+Z still works. Create, rename and delete a script; attach it to and detach it from a request; reorder and colour requests inside a script. |
| B3 | From an MCP client: the tool list shows the new names, and attaching or reordering needs approval as before. |
| C1 | The Scripts rail entry has its icon on Windows **and macOS** (icons need explicit size, see `AGENTS.md`). Rename and delete work from the context menu. |
| C2 | Attach two scripts to one request; bulk-attach a script to several selected requests; creating a script from the dialog adds it to the sidebar. Inside a script, delete one request and then several: they leave the script, are **not** in Trash, and are still in their collection. |
| C3 | Select a script: the editor shows its text. Type, wait, switch away and back: the text is saved. Restart: still saved. The WIP banner and donate link show. Selecting a request inside the script view keeps the editor (A2). |
| D1–D2 | Select a script: the editor is in the middle, WIP + "Attached to N requests" on the right. Type, switch script and back: saved. Attach a request from a collection view: the count goes up. Ctrl+Z it: down. Other views unchanged. |
| D3 | Attaching still works from the editor chips, the "Scripts…" menu and drag-and-drop. Del in a collection still moves to Trash. |

---

## Known issues (not caused by this work)

- `go test ./...` has 6 failures that already existed before Stage B (checked on the
  commit before B1): 3 in `commands/openapi`, `TestMakeGroupNamesUniqueRenamesDuplicates`
  in `database`, and 2 in `mcp`.
- The production frontend build fails: `requests-main-list.scss` is 572 bytes over the
  20 kB budget in `angular.json`. Because of that, `frontend/dist` can be left empty and
  `go build ./...` then fails on the embed; run `npx ng build --configuration development`
  first.
