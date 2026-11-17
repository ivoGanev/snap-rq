# MCP server

SnapRQ can expose a **curated** set of its commands to AI assistants over the
Model Context Protocol (MCP), so an assistant can organize requests: browse
collections, move, rename, tag, favourite, reorder, and so on.

Status: phase 1 (foundation + read tools). See "Phases" below.

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

## Risk classes

| Class | Meaning | Default |
|---|---|---|
| `read` | queries only | on |
| `organize` | recoverable edits: rename, move, reorder, tag, favourite, colour, duplicate, bulk edit, delete-to-trash, restore | on |
| `destructive` | cascading or permanent deletes | off |
| `external` | outbound network (`send_request`) | off |
| `sensitive` | environments, variables, cookie jar | off |

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
1. Foundation and read tools.
2. Organize tools (collections, requests incl. move/reorder/bulk, tags, favourites, trash).
3. Settings UI (toggle, port, token, per-class and per-tool switches).
4. Restricted tools (destructive, sensitive, `send_request` with `RunObservable`).
5. stdio shim, resources, prompts.
