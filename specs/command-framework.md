# Command Framework and Undo — High-Level Statement

## What we are building and why

All workspace mutations flow through a single **command history** in the Go
backend. It serves three purposes:

1. **Undo/redo** for every workspace-affecting mutation (create, delete,
   rename, edit, duplicate, move, reorder, favourite membership, icon/colour
   changes, bulk operations, imports).
2. **One choke point for mutation.** The app's state lives in SQLite and in
   the frontend's in-memory signal stores at the same time. The history is
   the only sanctioned write path, and after every change it tells the UI
   which entities to refresh, so every store stays consistent by
   construction, not by convention.
3. **A public operation vocabulary.** Commands are named, typed operations
   (`request.rename`, `collection.create`, ...). External parties (AI agent,
   CLI) will later trigger actions through these exact names and payloads.
   We are not building that surface now, but the contract — command name,
   payload shape, result, label — is first-class from day one so the
   automation surface is a transport detail, not a redesign.

## How it works

- A **command** is a named function that changes the database, plus a label
  for the undo menu. It runs inside one transaction.
- Before it runs, we save copies of every row it will touch. After it runs,
  we save copies again. Undo puts the before-copies back; redo puts the
  after-copies back. There is no reverse-command synthesis — restoring
  saved rows is robust for cascading deletes, bulk edits, and multi-table
  operations, and the trash-bin (snapshot + restore) is the same idea.
- Commands that cannot be taken back (sending a request: external side
  effects; response history and usage: append-only logs; settings:
  preferences; emptying the trash for good) set `Undoable: false`. They run
  through the same path but are not remembered, and they never disturb the
  undo list — sending a request must not wipe your undo history.
- Quick repeats of the same edit (typing a new name) merge into one step.
- Reads are untouched: query methods on the existing services stay as they
  are.

## Undo semantics

- Two lists (undo/redo) owned by the **backend**, so history survives any
  frontend reload; the lists are capped by the user's settings.
- A new remembered change makes redo impossible: it happened in a world the
  redo steps know nothing about.
- Steps are checked before being undone: if their rows were destroyed for
  good (e.g. the trash was emptied permanently), the step is dropped with a
  visible notice rather than corrupting state.
- One user action = one step, even when it touches many tables (curl
  import, tag rename, drag-reorder).
- UI state (selection, open tabs, scroll) is restored as a side effect of
  undo results using the step's entity refs. It never goes on the lists.
- Secrets safety: saved copies may hold previous environment-variable
  values, but they are never logged or shipped in events (events carry ids
  only; the frontend refetches). They persist only inside the app's own
  database (see crash-safe persistence below) — the same local threat model
  as the live table — and are bounded by the list caps.

## Rules that keep undo correct

- **Only ever unwind back-to-front.** The undo list is strictly
  reverse-order; nothing outside later steps can reference a saved row.
  The public interface exposes only Undo/Redo — never "undo step #N" —
  including for future AI callers.
- **Only changed columns go back.** Undoing an update writes back only the
  columns the command changed, so an interleaved send (which writes
  `status_code`/`response_id`) survives.
- **Columns have one owner.** Undoable commands never write the send
  flow's columns; the send flow never writes the user's columns.
- **Every table keeps AUTOINCREMENT.** A plain max(id)+1 table could reuse
  an id after an undo and silently rewire an old step onto a new row.
- **Destroying data for good forgets its steps.** Permanently emptying the
  trash drops every step that involved those rows — undo must never bring
  back something the user meant to destroy.
- **Failures are loud.** A conflict or a schema change that invalidates a
  saved copy fails the restore transaction; the step is skipped with a
  notice, never half-applied.

## Mutation classification

**Undoable** — create/delete/rename/edit/duplicate of: requests,
collections, projects, profiles, environments, environment variables,
favourite collections and their memberships, tags and their memberships,
appearances (icon/colour) and request colours; move between collections or
projects; drag-reorder in every view; all bulk field updates; curl imports
and trash restores.

**Not undoable — by category:**

| Category | Operations | Reason |
| --- | --- | --- |
| External side effects | sending/executing requests | You cannot un-send a `DELETE /users/5`. |
| Append-only history | response saves + trimming, usage tracking | History is a log; replaying old entries is wrong. |
| Filesystem | save response to file | Outside our transactional domain. |
| Preferences | app settings | Not workspace state; must not disturb undo history. |
| Explicitly permanent | emptying the trash permanently | The point is irreversibility; the affected steps are forgotten. |
| System maintenance | icon-reference cleanup | Upkeep, not user intent. |

Deleting a whole **project or profile** cascades across nearly every table;
these get full-snapshot undo in a later phase, and are non-undoable until
then.

## Crash-safe persistence (Phase 4)

The in-memory lists survive view reloads but not app crashes. Since a
committed mutation without its undo step is unrecoverable (the accidental
project delete), history will be persisted in **one table**, same
JSON-snapshot pattern as `binned_requests.snapshot_json`:

```sql
CREATE TABLE undo_history (
	seq           INTEGER PRIMARY KEY,   -- execution order
	command_name  TEXT NOT NULL,
	label         TEXT NOT NULL,
	merge_key     TEXT NOT NULL DEFAULT '',
	rows_json     TEXT NOT NULL,          -- saved before/after row copies
	created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	state         TEXT NOT NULL CHECK(state IN ('undo', 'redo'))
);
```

- **The history row is written in the SAME transaction as the mutation.**
  A crash can never separate a committed change from its undo step: either
  the project delete and its saved copies land together, or neither lands.
- Undo/redo restore their rows and flip `state` in one transaction.
- Column-name-keyed copies keep old steps usable across schema migrations
  (missing columns are simply not restored).

## Public interface direction (future, contract only)

A `CommandService` (Wails-bound) exposing `Execute(name, payload)`, `Undo`,
`Redo`, `History` (the undo-menu summary), `Stack` (the step list for the
inspector) and `ListCommands` (self-describing for automation). The
per-service mutation methods become thin dispatchers over the same command
names during migration and are deprecated once the frontend is converted.

## Non-goals for v1

- No transport for external callers (CLI/HTTP/agent) — contract only.
- No multi-user concerns, no time travel beyond redo.
