# Command Framework — End-to-End Flow Example

Companion to `command-framework.md`, matched to the code in
`backend/commands`. Example operation: **rename a request**
(`request.update`) — one change, one undo, one redo.

## The flow at a glance

```
user renames a request
  └─ frontend: commands.execute('request.update', {...})      [IPC]
       └─ History.Run: lock → begin transaction
            └─ command: rec.SaveRows("http_requests", id)   ← before copy
                        UPDATE ... SET name = ?
                        rec.Touched("request", "update", id)
            └─ History: save after copies, commit
                         remember the step (merge quick repeats)
            └─ emit 'command:changed' {entity:'request', ids:[id]}
                     'command:history' {undoSteps, redoSteps, labels}
       └─ frontend stores refresh those ids (targeted, no full reload)

user presses Ctrl+Z
  └─ CommandService.Undo()                                    [IPC]
       └─ History.Undo: take the newest step
                        check its rows still exist
                        put the before-copies back in one transaction
                        move the step to the redo list
                        emit the same events
       └─ stores refresh; the toast offers Redo
```

Commands that cannot be undone (send request, save response, record usage)
run through the same `Run` — same transaction, same events — but are not
remembered, so sending a request never disturbs the undo list.

## The command, as it is written today

```go
// backend/services/requestcommands.go
func (s *RequestService) requestUpdateCommand() *commands.Command {
	return &commands.Command{
		Name:     "request.update",
		Label:    "Update Request",
		Undoable: true,
		MergeKey: func(input json.RawMessage) string { // typing = one step
			var req models.HttpRequest
			json.Unmarshal(input, &req)
			return fmt.Sprintf("request.update:%d", req.ID)
		},
		Apply: func(tx *sql.Tx, rec *commands.Recorder, input json.RawMessage) (any, error) {
			var req models.HttpRequest
			json.Unmarshal(input, &req)

			rec.SaveRows("http_requests", req.ID) // copy before changing

			if err := s.execUpdateRequest(tx, req); err != nil { // user's columns only
				return nil, err
			}

			rec.Touched("request", "update", req.ID) // what the UI refreshes
			return s.getRequestWithProjectTx(tx, req.ID)
		},
	}
}
```

A cascading command (`request.delete` → trash bin) looks the same — it just
calls `rec.SaveRows` for the request and everything that dies with it
(responses, tag and favourite memberships), so undo puts the whole tree
back under the original ids.

## The three files of the package

| File | Contains |
| --- | --- |
| `commands.go` | `Command` (name, label, undoable, merge key, Apply) and `Recorder` (saving row copies) |
| `history.go` | `History`: `Run`, `Undo`, `Redo`, `Register`, `Status`, `Steps`, `ForgetRows` |
| `restore.go` | putting saved rows back: the `planRestore` split into update/insert/delete, all in one transaction |

`History.Undo` reads as the comment does: check the rows exist, update the
changed columns, insert the deleted rows back, delete the created rows —
one transaction, nothing half-applied. Events are emitted after the lock is
released so a busy window can never stall the next undo.

## Frontend

```ts
// core/services/command.service.ts — undo menu state from events
Events.On('command:history', ev => this.onStatus(ev.data as Status));
Events.On('command:changed', ev => this.changes.next(...)); // → StoreSync

// core/services/store-sync.service.ts — targeted refresh, no full reloads
// ids that exist are upserted into the request store; ids that vanished
// are dropped; only view-local lists (favourite/tag/recent/bin) reload.
```

## Why this holds together

- **SQLite + memory never drift:** one write path, one event, all stores
  reconcile from the same delta.
- **Undo correctness for cascades/bulk:** saved copies restore exact rows
  with original ids.
- **History survives reloads:** the lists live in the Go process.
- **Public vocabulary for free:** `request.update` + payload IS the future
  AI/CLI contract, and `Execute` is the only entry point outsiders need.
