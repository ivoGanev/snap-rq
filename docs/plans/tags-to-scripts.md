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

### Assumptions (confirm or correct)

- **A1:** Per-request **colours** inside a script are kept, like favourites and collections (only ordering was confirmed).
- **A2:** In the Scripts view the Inspector **always** shows the selected script's editor, even when a request row is selected. To see a response, the user opens the request from another view.
- **A3:** Script text is plain text with **JavaScript highlighting**, auto-saved after a short pause (like other editors in the app). Editing text is not part of the app's undo/redo history; CodeMirror's own undo covers it.
- **A4:** MCP tools are renamed (`add_tag` → `attach_script`, etc.). Any MCP client that calls the old names stops working. Acceptable before release.

---

## Target shape

```
┌─ sidebar rail ─┬─ group list ─────┬─ request list ──────────┬─ Inspector ───────────────┐
│ All            │ (collections /   │ requests of the active  │ switches on active view:  │
│ Recent         │  favourites /    │ group                   │  • response-detail        │
│ Collections    │  scripts)        │                         │  • binned-request-detail  │
│ Favourites     │                  │                         │  • script-detail (editor) │
│ Scripts  ◄ new │                  │                         │                           │
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

**Out of scope for now:** splitting `requests-main-list` (2,250 lines) into a generic,
column-configurable grid. Scripts no longer need their own list (D2), so this can wait. It is
listed under "Later" so it isn't forgotten.

---

## Steps

### Stage A: refactor, no visible change

- [ ] **A1. Inspector container.**
  Rename `request-panel` → `inspector`. Move the response part into `response-detail` and
  the trash restore/delete card (and its "restore to collection" dialog) into
  `binned-request-detail`. The Inspector only decides which one to show. The "Response"
  heading and the response count move into `response-detail`.
  _Files:_ `frontend/src/app/request-panel/*` → `frontend/src/app/inspector/*`, new
  `response-detail/`, `binned-request-detail/`, `app.html`, `app.ts`.

- [ ] **A2. Active view state.**
  Add `activeView` to `WorkspaceStateService`. Keep the old flags as computed values
  derived from it so callers keep working, then move the Inspector to `activeView`.
  _Files:_ `core/services/workspace-state.service.ts`, `inspector/*`, `request-groups/*`,
  `sidebar-rail/*`.

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
  scripts, or create one by name.
  _Files:_ `requests-main-list/*`.

- [ ] **C3. Script editor in the Inspector.**
  New `script-detail`: script name heading, CodeMirror editor (JavaScript mode, A3) with
  auto-save, and a WIP banner: "Scripts can't run yet. This feature is in progress, so if
  you'd like to see it sooner, consider supporting development." with a link to D4. The
  Inspector shows it whenever the active view is a script (A2).
  _Files:_ new `script-detail/`, `inspector/*`, `core/services/script.service.ts`.

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
| A1 | Right panel looks exactly as before in every view: response heading and count, trash card, "restore to collection" dialog when the original collection is gone, and the empty-state message. |
| A2 | Clicking each rail entry and each group shows the right content in the right panel. Switching quickly between Trash and a collection never leaves a stale trash card or response. |
| A3 | Sorting, reorder handles and colour controls appear only in collections, favourites and tags. Scroll position and selection are remembered per group. |
| B1 | Start the app on a copy of an existing database: it starts, tags are gone, all other data is intact. Starting it a second time doesn't fail. |
| B2 | Create, rename and delete a script; attach it to and detach it from a request; reorder and colour requests inside a script. |
| B3 | From an MCP client: the tool list shows the new names, and attaching or reordering needs approval as before. |
| C1 | The Scripts rail entry has its icon on Windows **and macOS** (icons need explicit size, see `AGENTS.md`). Rename and delete work from the context menu. |
| C2 | Attach two scripts to one request; bulk-attach a script to several selected requests; creating a script from the dialog adds it to the sidebar. |
| C3 | Select a script: the editor shows its text. Type, wait, switch away and back: the text is saved. Restart: still saved. The WIP banner and donate link show. Selecting a request inside the script view keeps the editor (A2). |
