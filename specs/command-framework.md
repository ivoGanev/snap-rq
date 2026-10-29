# Command Framework and Undo — High-Level Statement

## What we are building and why

All workspace mutations will flow through a single **command framework** in the Go
backend. It serves three purposes:

1. **Undo/redo** for every workspace-affecting mutation (create, delete, rename,
   edit, duplicate, move, reorder, favourite membership, icon/colour changes,
   bulk operations, imports).
2. **One choke point for mutation.** The app's state lives in SQLite and in the
   frontend's in-memory signal stores at the same time. Today every caller
   patches the stores it happens to know about (a request send patches three
   different stores by hand). The command bus will be the only sanctioned write
   path, and it will publish structured change events after commit so every
   store stays consistent by construction, not by convention.
3. **A public operation vocabulary.** Commands are named, typed operations
   (`request.rename`, `collection.create`, ...). External parties (AI agent,
   CLI) will later execute actions through these exact names and payloads. We
   are not building that surface now, but the contract — command name, payload
   shape, result, metadata — is first-class from day one so the automation
   surface is a transport detail, not a redesign.

## The command model

- A **command** = stable name + JSON-friendly typed payload + execution handler.
  Handlers run inside a single SQLite transaction and capture a **memento**
  (before/after state of every affected row, including cascade victims).
- Undo is **memento-based, not inverse-command-based**. Applying recorded state
  back is robust for cascading deletes, bulk updates, and multi-table macro
  operations where synthesising a reverse command is fragile. The existing
   trash-bin (snapshot + restore) is the proof-of-pattern we generalise.
- Command metadata carries: a human label (for the Edit menu: "Undo Rename
  Request"), a category (see classification below), a coalesce key (so ten
  keystrokes on one name become one undo entry), and the entity references it
  touched (for UI state restore).
- Reads are unaffected. Query methods on the existing services stay as they are.

## Undo semantics

- Two stacks (undo/redo) owned by the **backend**, so the history survives any
  frontend reload; the stacks are capped and memory-bounded.
- A new undoable command clears the redo stack. A **non-undoable command never
  touches either stack** — sending a request must not wipe your undo history.
- Undo entries are **validated at undo time**: if their target rows are gone
  (e.g. the user permanently emptied the bin that an undo would have restored
  from), the entry is skipped with a visible notice rather than corrupting
  state. This handles every invalidation case generically.
- One user action = one undo entry, even when it touches many tables
  (curl import, tag rename, drag-reorder). Multi-step IPC flows collapse into
  single atomic commands.
- UI state (selection, open tabs, scroll) is **restored as a side effect** of
  undo results using the entry's entity references. It never goes on the stack.
- Secrets safety: mementos may hold previous environment-variable values, but
  they are never logged or shipped in change events (events carry IDs; the
  frontend refetches). They persist only inside the app's own `undo_history`
  table (see below) — the same local-DB threat model as the live table — and
  are bounded by the stack caps.

## Unwind correctness rules

Memento restore is only safe under these rules; they are part of the
framework contract, not implementation details:

- **LIFO only.** Stack discipline is load-bearing: nothing outside later
  entries can reference a memented row, so reverse-order unwind is always
  referentially safe. The public interface therefore exposes only
  Undo/Redo — never "undo entry #N" — including for future AI callers.
- **Diff-based restore.** Undoing an update restores only the columns that
  differ between its Before/After images, so interleaved non-undoable
  writes to other columns (a send updating `status_code`/`response_id`)
  are never clobbered.
- **Column ownership.** Undoable commands never write columns owned by
  non-undoable paths (`status_code`, `response_id` belong to the send
  flow); the two write families must not overlap on any column.
- **AUTOINCREMENT everywhere.** Every memented table keeps AUTOINCREMENT;
  plain `max(id)+1` reuse could silently rewire an old entry onto a new row.
- **Reference-based purge.** Explicitly permanent operations (permanently
  emptying the bin) purge stack entries whose mementos reference the
  destroyed rows — undo must never resurrect something the user
  deliberately obliterated.
- **Loud failures.** UNIQUE conflicts or schema-migration damage make a
  restore fail its transaction; the entry is skipped with a visible
  notice, never partially applied. Persisted history (Phase 4) widens
  exposure to this and relies on the same containment.

## Mutation classification

**Undoable** — create/delete/rename/edit/duplicate of: requests, collections,
projects, profiles, environments, environment variables, favourite collections
and their memberships, tags and their memberships, appearances (icon/colour for
collections, favourites, tags) and request colours; move between collections or
projects; drag-reorder in every view; all bulk field updates; curl imports and
restores from the bin (deleting via the view is already "move to bin", so
delete+restore are a natural undo pair).

**Not undoable — by category:**

| Category | Operations | Reason |
| --- | --- | --- |
| External side effects | sending/executing requests | You cannot un-send a `DELETE /users/5`. |
| Append-only history | response saves + trimming, request-usage tracking, response edits/deletes | History is a log; replaying old entries is wrong. |
| Filesystem | save response to file | Outside our transactional domain. |
| Preferences | app settings (timeouts, SSL validation) | Not workspace state; changing them must not disturb undo history. |
| Explicitly permanent | permanently delete binned requests | The whole point is irreversibility; validation handles stale undo entries it invalidates. |
| System maintenance | icon-reference cleanup | Runs as upkeep, not as user intent. |

Deleting a whole **project or profile** cascades across nearly every table.
These get full-snapshot undo in a later phase; until the snapshot machinery
proves itself they are treated as non-undoable (rare, deliberate actions).

## Store synchronisation (the choke point payoff)

After a command commits, the bus emits one structured change event describing
what changed (entity type, IDs, operation). Frontend stores subscribe and apply
the delta uniformly. The ad-hoc "patch every store I know" code in the Angular
api-services is progressively removed. This is what actually guarantees the
SQLite/memory pair never drifts — including for the future external command
callers, who get correct UI updates for free through the same events.

## Public interface direction (future, contract only)

A `CommandService` (Wails-bound) exposing `Execute(name, payload)`, `Undo`,
`Redo`, `History` (labels for UI), and `ListCommands` (self-describing
registry for automation). The per-service mutation methods on the existing
Wails services become thin dispatchers over the same command names during
migration and are deprecated once the frontend is fully converted.

## Crash-safe persistence (Phase 4)

In-memory stacks survive view reloads but not app crashes. Since a committed
mutation without its undo entry is unrecoverable (the accidental project
delete), history is persisted in **one table**, same JSON-snapshot pattern as
`binned_requests.snapshot_json`:

```sql
CREATE TABLE undo_history (
	seq           INTEGER PRIMARY KEY,   -- execution order
	command_name  TEXT NOT NULL,
	label         TEXT NOT NULL,
	coalesce_key  TEXT NOT NULL DEFAULT '',
	memento_json  TEXT NOT NULL,          -- before/after row images
	created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	state         TEXT NOT NULL CHECK(state IN ('undo', 'redo'))
);
```

- **The history row is written in the SAME transaction as the mutation.**
  A crash can never separate a committed change from its undo entry: either
  the project delete and its restore memento land together, or neither lands.
  This single rule is what makes the crash scenario safe.
- Undo/redo apply their memento and flip `state` in one transaction — a crash
  mid-undo leaves no half-state.
- A new undoable command deletes all `redo` rows and appends the next `seq`;
  the redo fork is discarded exactly as in memory. Invariant: every `redo`
  row's seq is above every `undo` row's.
- Coalescing updates the newest row in place; entry/byte caps evict oldest
  rows; startup rebuilds both stacks by reading the (capped, small) table.
- Memento maps are keyed by column name, so old entries tolerate later schema
  migrations (extra/missing columns are simply not restored).

Cost: every undoable command also writes its memento to disk — bounded by the
caps, and the bin already writes comparable snapshots today.

## Non-goals for v1

- No transport for external callers (CLI/HTTP/agent) — contract only.
- No multi-user concerns, no time travel beyond redo.
