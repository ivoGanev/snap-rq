# Command Framework — End-to-End Flow Example

Companion to `command-framework.md`. Illustrative code: the shapes are the
contract, details will move during implementation. Example operation:
**rename a request** (`request.rename`) — one update, one undo, one redo.

## The flow at a glance

```
user renames request
  └─ frontend: commands.execute('request.rename', {id, name})     [IPC]
       └─ Bus: lock → lookup handler → BEGIN TX
            └─ handler: Snapshot('http_requests', id)  → BEFORE image
                        UPDATE ... SET name = ?
                        Affected('request','update', id)
            └─ Bus: re-read same rows        → AFTER image
                     COMMIT
                     push undo entry ("Rename Request", coalesce by id+field)
                     clear redo
            └─ Bus: emit 'command:changed' {entity:'request', op:'update', ids:[id]}
                     emit 'command:history' {undoDepth, redoDepth, nextUndoLabel}
       └─ every frontend store applies the delta → UI + undo indicator update

user presses Ctrl+Z
  └─ frontend: CommandService.Undo()                              [IPC]
       └─ Bus: pop entry → validate targets still exist
                apply BEFORE memento in one TX (restore exact rows/ids)
                push to redo stack
                emit 'command:changed' + 'command:history'
       └─ frontend stores apply delta;
          selection restored as a SIDE EFFECT from the result's refs
```

Non-undoable commands (send request, save response, record usage) run through
the same `Execute` path — same transaction, same events — but touch no stack,
so sending a request never disturbs undo history.

## Backend kernel

```go
// backend/commands/bus.go
package commands

type Category int

const (
	Undoable   Category = iota // goes on the undo stack
	SideEffect                 // executes, never touches stacks (send request)
	System                     // append-only writes (responses, usage, settings)
)

// Command is one named mutation. Registered once at startup.
type Command struct {
	Name        string // stable public name, e.g. "request.rename"
	Label       string // undo-UI label, e.g. "Rename Request"
	Category    Category
	CoalesceKey func(payload json.RawMessage) string // "" = never coalesce
	Handler     func(ctx *Context, payload json.RawMessage) (any, error)
}

// Context gives the handler its transaction plus memento/event helpers.
type Context struct {
	Tx       *sql.Tx
	captures []capture // (table, ids) pairs snapshotted before mutation
	affected []Change  // entity/op/id refs for events and UI-state restore
}

// Snapshot records rows BEFORE the handler mutates them. First capture per
// (table, id) wins; the bus re-reads the same rows for the AFTER image.
func (c *Context) Snapshot(table string, ids ...int64) error

// Affected declares which entities the command touched (for change events
// and the undo result). IDs only — never row values, so secrets never ride
// the event bus.
func (c *Context) Affected(entity, op string, ids ...int64)

type Row struct {
	ID     int64          `json:"id"`
	Values map[string]any `json:"values"` // column -> value
}

// Memento is the before/after image pair of every row the command touched,
// including cascade victims (a collection delete snapshots requests,
// responses, favourite items, tags links, appearance rows).
type Memento struct {
	Before map[string]map[int64]Row
	After  map[string]map[int64]Row
}

type Entry struct {
	Command     string
	Label       string
	At          time.Time
	CoalesceKey string
	Memento     *Memento
}
```

```go
// backend/commands/execute.go
func (b *Bus) Execute(name string, payload json.RawMessage) (any, error) {
	b.mu.Lock()         // single writer: commands serialize (SQLite anyway)
	defer b.mu.Unlock()

	cmd, ok := b.registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown command %q", name)
	}

	tx, err := b.db.Begin()
	if err != nil { return nil, err }
	defer tx.Rollback()

	ctx := &Context{Tx: tx}
	result, err := cmd.Handler(ctx, payload)
	if err != nil { return nil, err }

	if cmd.Category == Undoable {
		// AFTER image is read inside the same tx, so it matches the commit.
		memento, err := ctx.buildMemento(tx)
		if err != nil { return nil, err }
		if err := tx.Commit(); err != nil { return nil, err }
		b.push(cmd, payload, memento)
	} else {
		if err := tx.Commit(); err != nil { return nil, err }
	}

	b.emit(ctx) // 'command:changed' + 'command:history'
	return result, nil
}

func (b *Bus) push(cmd *Command, payload json.RawMessage, m *Memento) {
	key := ""
	if cmd.CoalesceKey != nil { key = cmd.CoalesceKey(payload) }

	// Coalescing: ten keystrokes on one name = one undo entry.
	if key != "" && len(b.undo) > 0 {
		top := b.undo[len(b.undo)-1]
		if top.CoalesceKey == key && time.Since(top.At) < 750*time.Millisecond {
			top.Memento.After = m.After // keep oldest Before, newest After
			top.At = time.Now()
			return
		}
	}

	b.redo = nil // new undoable action forks history
	b.undo = append(b.undo, &Entry{ /* name, label, key, memento... */ })
	if len(b.undo) > maxStack { b.undo = b.undo[1:] }
}
```

Undo / redo are the same operation mirrored:

```go
// backend/commands/undo.go
type UndoResult struct {
	Command string
	Label   string
	Refs    map[string][]int64 // entity -> ids, for UI-state restore
}

func (b *Bus) Undo() (UndoResult, error) {
	b.mu.Lock(); defer b.mu.Unlock()
	if len(b.undo) == 0 { return UndoResult{}, errors.New("nothing to undo") }

	entry := b.undo[len(b.undo)-1]
	b.undo = b.undo[:len(b.undo)-1]

	if err := b.validate(entry); err != nil {
		// Target rows are gone (e.g. bin was permanently emptied). Policy:
		// drop the stale entry, surface a notice, keep going.
		return b.Undo()
	}

	// apply(Before, After): rows only in After were created -> DELETE;
	// rows in Before are restored with their ORIGINAL ids (FK children were
	// snapshotted too, so the whole subtree snaps back consistently).
	if err := b.apply(entry.Memento.Before, entry.Memento.After); err != nil {
		b.undo = append(b.undo, entry) // restore position; tx already rolled back
		return UndoResult{}, err
	}

	b.redo = append(b.redo, entry)
	b.emitFor(entry)
	return UndoResult{Command: entry.Command, Label: entry.Label, Refs: entry.refs()}, nil
}

func (b *Bus) apply(target, opposite map[string]map[int64]Row) error {
	tx, err := b.db.Begin()
	if err != nil { return err }
	defer tx.Rollback()

	for table, rows := range target { // parents first (capture order)
		for _, row := range rows {
			// INSERT OR REPLACE INTO table (cols...) VALUES (...) — original ids
		}
	}
	for table, rows := range opposite {
		for id := range rows {
			if _, kept := target[table][id]; !kept {
				// DELETE FROM table WHERE id = ? — rows the command created
			}
		}
	}
	return tx.Commit()
}
```

## A concrete command

```go
// backend/commands/catalog_requests.go
var RenameRequest = &Command{
	Name:  "request.rename",
	Label: "Rename Request",
	CoalesceKey: func(p json.RawMessage) string {
		var pl struct{ ID int64 `json:"id"` }
		_ = json.Unmarshal(p, &pl)
		return fmt.Sprintf("request.rename:%d", pl.ID)
	},
	Handler: func(ctx *Context, payload json.RawMessage) (any, error) {
		var pl struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(payload, &pl); err != nil { return nil, err }
		if pl.ID == 0 || strings.TrimSpace(pl.Name) == "" {
			return nil, errors.New("id and name are required")
		}

		if err := ctx.Snapshot("http_requests", pl.ID); err != nil { return nil, err }

		if _, err := ctx.Tx.Exec(
			`UPDATE http_requests SET name = ? WHERE id = ?`, pl.Name, pl.ID,
		); err != nil { return nil, err }

		ctx.Affected("request", "update", pl.ID)
		return queryRequest(ctx.Tx, pl.ID) // same row shape the frontend gets today
	},
}
```

A cascade command (`collection.delete`) looks identical — its handler just
calls `Snapshot` for every affected table (requests, responses, favourite
items, request_tags, appearance) before deleting, so undo restores the whole
tree. The trash-bin flow (`request.delete` → bin, `bin.restore`) becomes two
normal commands; its snapshot table is eventually replaced by the memento.

## Wails surface + legacy bridge

```go
// backend/services/commandservice.go — the future public interface
type CommandService struct{ bus *commands.Bus }

func (s *CommandService) Execute(name, payload string) (ExecuteResult, error) {
	out, err := s.bus.Execute(name, json.RawMessage(payload))
	raw, _ := json.Marshal(out)
	return ExecuteResult{Result: raw}, err
}
func (s *CommandService) Undo() (commands.UndoResult, error) { return s.bus.Undo() }
func (s *CommandService) Redo() (commands.UndoResult, error) { return s.bus.Redo() }
func (s *CommandService) History() commands.HistorySnapshot  { /* depths + next label */ }
func (s *CommandService) ListCommands() []commands.CommandInfo { /* for future AI callers */ }
```

During migration the existing per-service methods become thin dispatchers, so
the frontend keeps working untouched:

```go
// backend/services/requestservice.go
func (s *RequestService) UpdateRequest(req models.HttpRequest) (models.HttpRequest, error) {
	payload, _ := json.Marshal(req)
	out, err := s.bus.Execute("request.update", payload)
	return out.(models.HttpRequest), err
}
```

Registration in `main.go`:

```go
bus := commands.NewBus(db)
bus.Register(commands.RenameRequest /* , ... */)
// services get the bus; CommandService joins application.NewService(...)
```

## Frontend

```ts
// core/services/command.service.ts
@Injectable({ providedIn: 'root' })
export class CommandService {
  readonly undoDepth = signal(0);
  readonly redoDepth = signal(0);
  readonly nextUndoLabel = signal('');
  private readonly changes = new Subject<CommandChange>();
  readonly change$ = this.changes.asObservable();

  constructor() {
    Events.On('command:changed', ev => this.changes.next(ev.data as CommandChange));
    Events.On('command:history', ev => {
      const h = ev.data as HistorySnapshot;
      this.undoDepth.set(h.undoDepth);
      this.redoDepth.set(h.redoDepth);
      this.nextUndoLabel.set(h.nextUndoLabel);
    });
  }

  execute<T>(name: string, payload: object): Promise<T> {
    return Services.CommandService.Execute(name, JSON.stringify(payload))
      .then(r => r.result as T);
  }

  undo() { return Services.CommandService.Undo(); }
  redo() { return Services.CommandService.Redo(); }
}
```

```ts
// request.service.ts — migrated: no more ad-hoc patching
constructor(commands: CommandService) {
  commands.change$
    .pipe(filter(c => c.entity === 'request'))
    .subscribe(c => this.applyChange(c));
}

private applyChange(c: CommandChange): void {
  switch (c.op) {
    case 'delete':  this.removeManyFromRequests(c.ids); break;
    case 'update':
    case 'create':  void this.refreshTouched(c.ids);    break; // refetch summaries
  }
}
```

The hand-rolled triple-store patch after a send (`workspace-state.service.ts`
`sendRequest`) disappears — the change event reaches every store uniformly,
including for future external command callers.

```ts
// app.component.ts — hotkeys + UI-state restore as a side effect
@HostListener('window:keydown.control.z', ['$event'])
async onUndo(e: KeyboardEvent) {
  e.preventDefault();
  const r = await this.commands.undo();
  const reqId = r.refs.request?.[0]; // refs come from ctx.Affected(...)
  if (reqId && this.workspace.selectedRequest()?.id !== reqId) {
    this.workspace.selectRequestById(reqId); // reselect, never on the stack
  }
}
```

## Why this holds together

- **SQLite + memory never drift:** one write path, one event, all stores
  reconcile from the same delta.
- **Undo correctness for cascades/bulk:** mementos restore exact rows with
  original ids; no fragile reverse commands.
- **History survives reloads:** the stacks live in the Go process.
- **Public vocabulary for free:** `request.rename` + payload IS the future
  AI/CLI contract — `ListCommands` is self-documenting, and Execute is the
  only entry point outsiders need.
