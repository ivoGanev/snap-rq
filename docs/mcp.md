# MCP server

SnapRQ can expose a **curated** set of its commands to AI assistants over the
Model Context Protocol (MCP), so an assistant can organize requests: browse
collections, move, rename, tag, favourite, reorder, and so on.

Status: phases 1-2 (foundation, read tools, organize tools). See "Phases" below.

## Principles

1. **Allowlist, not a mirror.** MCP never exposes the command catalog. Only the
   tools listed in `backend/mcp/catalog.go` exist, and each is hard-wired to
   one command id. There is no "run any command" tool. A command added to
   `catalog.All` is invisible to MCP until someone writes a tool for it.
2. **One door.** Tools call `Runner.Run(...)`, exactly like the UI's
   `CommandService`. Locking, undo log and `command:changed` events are
   unchanged, so MCP changes show up live in the UI and are undoable with the
   shared undo stack (the app behaves as if there were one user).
3. **Restrictable.** Every tool has a risk class. Classes and individual
   tools can be switched on or off. Disabled tools are not listed to the
   client and are rejected if called.
4. **Opt-in and local.** Off by default. Binds to `127.0.0.1` only, requires a
   bearer token, and rejects non-loopback `Host`/`Origin` headers.

## Architecture

```
MCP client --HTTP+token--> backend/mcp.Server --> tool catalog + Policy
                                                      |
Angular UI --> CommandService ------------------> commands.Runner --> SQLite
                                                      |
                                           command:changed events --> UI
```

Package `backend/mcp` (imports the SDK as `sdk`):

| File | Role |
|---|---|
| `catalog.go` | Data only: `ToolMeta` (name, description, risk class, command id), `NeverExposed`, catalog validation |
| `policy.go` | Risk class defaults + per-tool overrides |
| `auth.go` | Token generation and the HTTP guard (loopback Host/Origin, bearer token) |
| `store.go` | Persistence in `app_settings` |
| `server.go` | Lifecycle, SDK server, live tool sync |
| `tools_read.go` | Tool bindings (typed inputs) for read tools |
| `tools_organize.go` | Typed inputs and bindings for organize tools, incl. the two composed tools (`create_request`, `move_requests`) |

## Tools (phase 2)

All read tools are `list_*` / `get_request`. Organize tools:

| Area | Tools | Undoable |
|---|---|---|
| Collections | `create_collection`, `rename_collection` | yes |
| Requests | `create_request`, `duplicate_request`, `delete_requests` (to trash), `restore_from_trash`, `bulk_edit_requests`, `reorder_requests` | yes |
| Moving | `move_requests` (reads each request, writes it back with a new `collection_id`) | **no** (same as moving in the UI; move back to revert) |
| Tags | `add_tag`, `remove_tag`, `rename_tag`, `reorder_tagged_requests`, `set_tag_request_colors` | yes |
| Favourites | `create_favourite`, `rename_favourite`, `add_to_favourite`, `remove_from_favourite`, `reorder_favourite_requests`, `set_favourite_request_colors` | yes |

Deliberately left out for now:

- **Free-form request editing** (`request.update`). It overwrites every column and is not undoable, so a partial edit would need read-merge-write. `bulk_edit_requests` covers one field at a time and is undoable.
- **Appearance tools** (collection, tag, favourite icons/colours). The icon vocabulary lives in the frontend, so a tool cannot validate values yet.
- Note on undo merging: quick repeats of the same command on the same rows share one undo step, so several `bulk_edit_requests` calls on different fields within 750 ms undo only the first field. Do one field per call and do not chain them.

## Risk classes

| Class | Meaning | Default |
|---|---|---|
| `read` | queries only | on |
| `organize` | recoverable edits: rename, move, reorder, tag, favourite, colour, duplicate, bulk edit, delete-to-trash, restore | on |
| `destructive` | cascading or permanent deletes | off |
| `external` | outbound network (`send_request`) | off |
| `sensitive` | variable values, cookie jar | off (not planned) |

Resolution: per-tool override in `mcp_tool_overrides`, else the class default.

Never exposed, whatever the settings (`NeverExposed`): `settings.update`,
`response.save_to_file`, `icon.cleanup`. Undo/redo are not exposed either.

## Settings (`app_settings` keys, JSON-encoded values)

| Key | Meaning |
|---|---|
| `mcp_enabled` | start the server on launch |
| `mcp_port` | loopback port (default 47821) |
| `mcp_token` | bearer token (generated on first start) |
| `mcp_tool_overrides` | `{"tool_name": true/false}` |

The typed `settings.get` / `settings.update` commands only read and write
their own keys, so the token never reaches the frontend through them.

## Trying it in dev (before the Settings UI exists)

Start the app with `SNAPRQ_MCP=1` (does not persist anything). The log prints
the address and token. Example for Claude Code:

```
claude mcp add --transport http snap-rq http://127.0.0.1:47821/mcp --header "Authorization: Bearer <token>"
```

## Adding a tool

1. Add a `ToolMeta` to `Catalog` (pick the risk class deliberately).
2. Add a typed input struct and a `binding` in `tools_*.go`.
3. Update the exact-names list in `catalog_test.go` (the allowlist test).

## Known caveats

- A request's multipart body can reference a local file (`file_path`) and
  `send_request` reads it. Organize tools can edit bodies, so keep
  `send_request` off until a mitigation exists (confirmation prompt and/or
  stripping `file_path` from MCP-written bodies).
- Tool output is capped (see `maxOutputBytes`); large responses must be
  narrowed by the caller.
- Live tool changes apply to new calls immediately (policy is re-checked on
  every call); clients may need to re-list tools.

## Phases

0. Spec and branch (this file).
1. Foundation and read tools. (done)
2. Organize tools (collections, requests incl. move/reorder/bulk, tags, favourites, trash). (done)
3. Settings UI (toggle, port, token, per-class and per-tool switches).
4. Restricted tools (destructive, sensitive, `send_request` with `RunObservable`).
5. stdio shim, resources, prompts.

## Settings UI (phase 3)

Settings → **MCP server…** opens an overlay (`frontend/src/app/mcp-settings`) with:

- the enable switch (stored in `app_settings`; the server starts and stops immediately) and a status line (running URL or the start error, e.g. port in use);
- the port (1024–65535; a running server restarts on the new port);
- the token: hidden by default, show / copy / rotate (rotating disconnects all clients);
- a Copy URL button (no client snippets: they would put the token on screen, and any MCP client needs only the URL and the token);
- the tool list grouped by risk class, with a switch per class and per tool, and Reset for tools that differ from their class default.

Backend: `backend/mcp/service.go` (`SettingsService`, registered in `main.go`). Class switches write a per-tool override for every tool in the class. Bindings in `frontend/bindings/snap-rq/backend/mcp` were written by hand with the same ids the generator uses; run `wails3 generate bindings -ts -i -clean=true` to replace them.

## Variables

`list_environments` and `list_variables` are read tools. `list_variables` returns names only (`id`, `name`); values are dropped in Go (`tools_variables.go`), covered by a leak test. Decision: the AI may know which variables exist and use `{{name}}`, never read the value. There is no tool that returns values.

## Masking secrets (on by default)

Setting `mcp_mask_secrets` (absent = on) plus `mcp_mask_extra` (the user's own names). When on, everything a tool returns passes through `Server.present`, which replaces secret values with `[REDACTED]` (`mask.go`):

- headers (`request_headers`, `headers`): `Authorization`, `Cookie`, `Set-Cookie`, `X-Api-Key`, anything containing `token`, `secret`, `password`, …;
- URLs: `user:pass@` passwords and sensitive query parameters;
- bodies: string values under sensitive JSON keys (layout kept), sensitive form-encoded fields, and form-data entries (`body_form`). Plain-text bodies are not touched;
- saved responses use the same rules.

`{{variable}}` placeholders are never masked. Masking applies to output only: tools that read and write back (`move_requests`) read the real data, so stored secrets are never overwritten. As a second guard, any tool argument containing `[REDACTED]` is refused while masking is on. Turning masking off in Settings asks for confirmation.

Limits: matching is by name, so a secret in an oddly named field or inside free text is not caught; add its name under "extra names".

The settings screen lists the always-hidden names (`BuiltinSecretNames`, returned by `SettingsService.Masking`) so the user can see what is covered. Only the checkbox toggles the enable and masking switches, not their text.
