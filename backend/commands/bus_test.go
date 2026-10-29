package commands

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	_ "modernc.org/sqlite"
)

func newTestBus(t *testing.T) (*Bus, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening in-memory db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(`
		CREATE TABLE widgets (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			color TEXT,
			status INTEGER NOT NULL DEFAULT 0
		)
	`); err != nil {
		t.Fatalf("creating test schema: %v", err)
	}
	return NewBus(db), db
}

func widgetCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM widgets").Scan(&count); err != nil {
		t.Fatalf("counting widgets: %v", err)
	}
	return count
}

func widgetName(t *testing.T, db *sql.DB, id int64) string {
	t.Helper()
	var name string
	if err := db.QueryRow("SELECT name FROM widgets WHERE id = ?", id).Scan(&name); err != nil {
		t.Fatalf("reading widget %d: %v", id, err)
	}
	return name
}

func widgetStatus(t *testing.T, db *sql.DB, id int64) int {
	t.Helper()
	var status int
	if err := db.QueryRow("SELECT status FROM widgets WHERE id = ?", id).Scan(&status); err != nil {
		t.Fatalf("reading widget status %d: %v", id, err)
	}
	return status
}

func renameWidgetCommand() *Command {
	return &Command{
		Name:     "widget.rename",
		Label:    "Rename Widget",
		Category: Undoable,
		CoalesceKey: func(p json.RawMessage) string {
			var pl struct {
				ID int64 `json:"id"`
			}
			if json.Unmarshal(p, &pl) != nil || pl.ID == 0 {
				return ""
			}
			return fmt.Sprintf("widget.rename:%d", pl.ID)
		},
		Handler: func(ctx *Context, payload json.RawMessage) (any, error) {
			var pl struct {
				ID   int64  `json:"id"`
				Name string `json:"name"`
			}
			if err := json.Unmarshal(payload, &pl); err != nil {
				return nil, err
			}
			if err := ctx.Snapshot("widgets", pl.ID); err != nil {
				return nil, err
			}
			if _, err := ctx.Tx.Exec("UPDATE widgets SET name = ? WHERE id = ?", pl.Name, pl.ID); err != nil {
				return nil, err
			}
			ctx.Affected("widget", "update", pl.ID)
			return pl.Name, nil
		},
	}
}

func mustExec(t *testing.T, bus *Bus, name string, payload string) any {
	t.Helper()
	out, err := bus.Execute(name, json.RawMessage(payload))
	if err != nil {
		t.Fatalf("executing %s: %v", name, err)
	}
	return out
}

func TestUndoRestoresPreviousValue(t *testing.T) {
	bus, db := newTestBus(t)
	if err := bus.Register(renameWidgetCommand()); err != nil {
		t.Fatalf("registering: %v", err)
	}
	if _, err := db.Exec("INSERT INTO widgets (name) VALUES ('original')"); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	mustExec(t, bus, "widget.rename", `{"id":1,"name":"renamed"}`)
	if widgetName(t, db, 1) != "renamed" {
		t.Fatalf("expected rename applied")
	}

	result, err := bus.Undo()
	if err != nil {
		t.Fatalf("undoing: %v", err)
	}
	if result.Label != "Rename Widget" {
		t.Errorf("expected label in undo result, got %q", result.Label)
	}
	if widgetName(t, db, 1) != "original" {
		t.Fatalf("expected original name restored, got %q", widgetName(t, db, 1))
	}
	if bus.History().UndoDepth != 0 || bus.History().RedoDepth != 1 {
		t.Errorf("expected depths 0/1, got %d/%d", bus.History().UndoDepth, bus.History().RedoDepth)
	}

	if _, err := bus.Redo(); err != nil {
		t.Fatalf("redoing: %v", err)
	}
	if widgetName(t, db, 1) != "renamed" {
		t.Fatalf("expected redo to re-apply rename, got %q", widgetName(t, db, 1))
	}
}

// Regression test for the column-clobbering hazard: an undo must restore
// only the columns its command changed, so an interleaved write to another
// column (a send updating status) survives.
func TestUndoPreservesInterleavedWritesToOtherColumns(t *testing.T) {
	bus, db := newTestBus(t)
	if err := bus.Register(renameWidgetCommand()); err != nil {
		t.Fatalf("registering: %v", err)
	}
	if _, err := db.Exec("INSERT INTO widgets (name) VALUES ('original')"); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	mustExec(t, bus, "widget.rename", `{"id":1,"name":"renamed"}`)

	// Interleaved non-undoable write, e.g. the send flow updating status.
	if _, err := db.Exec("UPDATE widgets SET status = 500 WHERE id = 1"); err != nil {
		t.Fatalf("interleaving write: %v", err)
	}

	if _, err := bus.Undo(); err != nil {
		t.Fatalf("undoing: %v", err)
	}

	if widgetName(t, db, 1) != "original" {
		t.Errorf("expected name restored, got %q", widgetName(t, db, 1))
	}
	if got := widgetStatus(t, db, 1); got != 500 {
		t.Errorf("expected interleaved status 500 preserved, got %d", got)
	}
}

func TestUndoOfCreateDeletesAndRedoRestoresOriginalID(t *testing.T) {
	bus, db := newTestBus(t)
	if err := bus.Register(&Command{
		Name:     "widget.create",
		Label:    "Create Widget",
		Category: Undoable,
		Handler: func(ctx *Context, payload json.RawMessage) (any, error) {
			var pl struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(payload, &pl); err != nil {
				return nil, err
			}
			result, err := ctx.Tx.Exec("INSERT INTO widgets (name) VALUES (?)", pl.Name)
			if err != nil {
				return nil, err
			}
			id, err := result.LastInsertId()
			if err != nil {
				return nil, err
			}
			if err := ctx.CaptureNew("widgets", id); err != nil {
				return nil, err
			}
			ctx.Affected("widget", "create", id)
			return id, nil
		},
	}); err != nil {
		t.Fatalf("registering: %v", err)
	}

	out := mustExec(t, bus, "widget.create", `{"name":"made"}`)
	createdID, ok := out.(int64)
	if !ok {
		t.Fatalf("expected int64 result, got %T", out)
	}
	if widgetCount(t, db) != 1 {
		t.Fatalf("expected 1 widget after create")
	}

	if _, err := bus.Undo(); err != nil {
		t.Fatalf("undoing: %v", err)
	}
	if widgetCount(t, db) != 0 {
		t.Fatalf("expected create undone")
	}

	if _, err := bus.Redo(); err != nil {
		t.Fatalf("redoing: %v", err)
	}
	if widgetCount(t, db) != 1 {
		t.Fatalf("expected redo to re-create the widget")
	}
	var redoID int64
	if err := db.QueryRow("SELECT id FROM widgets").Scan(&redoID); err != nil {
		t.Fatalf("reading redone widget: %v", err)
	}
	if redoID != createdID {
		t.Errorf("expected redo to restore original id %d, got %d", createdID, redoID)
	}
}

func TestUndoOfDeleteRestoresRowWithOriginalID(t *testing.T) {
	bus, db := newTestBus(t)
	if err := bus.Register(&Command{
		Name:     "widget.delete",
		Label:    "Delete Widget",
		Category: Undoable,
		Handler: func(ctx *Context, payload json.RawMessage) (any, error) {
			var pl struct {
				ID int64 `json:"id"`
			}
			if err := json.Unmarshal(payload, &pl); err != nil {
				return nil, err
			}
			if err := ctx.Snapshot("widgets", pl.ID); err != nil {
				return nil, err
			}
			if _, err := ctx.Tx.Exec("DELETE FROM widgets WHERE id = ?", pl.ID); err != nil {
				return nil, err
			}
			ctx.Affected("widget", "delete", pl.ID)
			return nil, nil
		},
	}); err != nil {
		t.Fatalf("registering: %v", err)
	}

	result, err := db.Exec("INSERT INTO widgets (name, color) VALUES ('keep-me', 'red')")
	if err != nil {
		t.Fatalf("seeding: %v", err)
	}
	id, _ := result.LastInsertId()

	mustExec(t, bus, "widget.delete", fmt.Sprintf(`{"id":%d}`, id))
	if widgetCount(t, db) != 0 {
		t.Fatalf("expected delete applied")
	}

	if _, err := bus.Undo(); err != nil {
		t.Fatalf("undoing: %v", err)
	}
	if widgetCount(t, db) != 1 {
		t.Fatalf("expected delete undone")
	}
	var name, color string
	var restoredID int64
	if err := db.QueryRow("SELECT id, name, color FROM widgets").Scan(&restoredID, &name, &color); err != nil {
		t.Fatalf("reading restored widget: %v", err)
	}
	if restoredID != id || name != "keep-me" || color != "red" {
		t.Errorf("expected original row restored, got id=%d name=%q color=%q", restoredID, name, color)
	}
}

func TestCoalescingMergesRapidSameKeyCommands(t *testing.T) {
	bus, db := newTestBus(t)
	if err := bus.Register(renameWidgetCommand()); err != nil {
		t.Fatalf("registering: %v", err)
	}
	if _, err := db.Exec("INSERT INTO widgets (name) VALUES ('original')"); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	mustExec(t, bus, "widget.rename", `{"id":1,"name":"a"}`)
	mustExec(t, bus, "widget.rename", `{"id":1,"name":"b"}`)

	if got := bus.History().UndoDepth; got != 1 {
		t.Fatalf("expected coalesced depth 1, got %d", got)
	}

	// One undo must jump back to the oldest before-image, not the middle.
	if _, err := bus.Undo(); err != nil {
		t.Fatalf("undoing: %v", err)
	}
	if widgetName(t, db, 1) != "original" {
		t.Errorf("expected coalesced undo to restore original, got %q", widgetName(t, db, 1))
	}
}

func TestNewUndoableCommandClearsRedoStack(t *testing.T) {
	bus, db := newTestBus(t)
	if err := bus.Register(renameWidgetCommand()); err != nil {
		t.Fatalf("registering: %v", err)
	}
	if _, err := db.Exec("INSERT INTO widgets (name) VALUES ('original')"); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	mustExec(t, bus, "widget.rename", `{"id":1,"name":"a"}`)
	if _, err := bus.Undo(); err != nil {
		t.Fatalf("undoing: %v", err)
	}
	if bus.History().RedoDepth != 1 {
		t.Fatalf("expected redo depth 1")
	}

	// Different coalesce key (different widget) so this cannot merge.
	if _, err := db.Exec("INSERT INTO widgets (name) VALUES ('other')"); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	mustExec(t, bus, "widget.rename", `{"id":2,"name":"b"}`)

	if got := bus.History().RedoDepth; got != 0 {
		t.Errorf("expected new undoable command to clear redo, got %d", got)
	}
}

func TestSystemCommandsNeverTouchStacks(t *testing.T) {
	bus, db := newTestBus(t)
	if err := bus.Register(&Command{
		Name:     "widget.status",
		Label:    "Set Widget Status",
		Category: System,
		Handler: func(ctx *Context, payload json.RawMessage) (any, error) {
			var pl struct {
				ID     int64 `json:"id"`
				Status int   `json:"status"`
			}
			if err := json.Unmarshal(payload, &pl); err != nil {
				return nil, err
			}
			if _, err := ctx.Tx.Exec("UPDATE widgets SET status = ? WHERE id = ?", pl.Status, pl.ID); err != nil {
				return nil, err
			}
			return nil, nil
		},
	}); err != nil {
		t.Fatalf("registering: %v", err)
	}
	if err := bus.Register(renameWidgetCommand()); err != nil {
		t.Fatalf("registering: %v", err)
	}
	if _, err := db.Exec("INSERT INTO widgets (name) VALUES ('original')"); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	mustExec(t, bus, "widget.rename", `{"id":1,"name":"renamed"}`)
	if _, err := bus.Undo(); err != nil {
		t.Fatalf("undoing: %v", err)
	}

	// A system write between undos must not wipe the redo stack.
	mustExec(t, bus, "widget.status", `{"id":1,"status":500}`)

	if got := bus.History().RedoDepth; got != 1 {
		t.Errorf("expected system command to leave redo stack intact, got %d", got)
	}
	if got := bus.History().UndoDepth; got != 0 {
		t.Errorf("expected system command to leave undo stack empty, got %d", got)
	}
	if _, err := bus.Redo(); err != nil {
		t.Fatalf("expected redo to still work: %v", err)
	}
}

func TestLimitTrimsOldestEntries(t *testing.T) {
	bus, db := newTestBus(t)
	bus.SetLimitProvider(func() int { return 2 })
	if err := bus.Register(&Command{
		Name:     "widget.color",
		Label:    "Colour Widget",
		Category: Undoable,
		Handler: func(ctx *Context, payload json.RawMessage) (any, error) {
			var pl struct {
				ID    int64  `json:"id"`
				Color string `json:"color"`
			}
			if err := json.Unmarshal(payload, &pl); err != nil {
				return nil, err
			}
			if err := ctx.Snapshot("widgets", pl.ID); err != nil {
				return nil, err
			}
			if _, err := ctx.Tx.Exec("UPDATE widgets SET color = ? WHERE id = ?", pl.Color, pl.ID); err != nil {
				return nil, err
			}
			return nil, nil
		},
	}); err != nil {
		t.Fatalf("registering: %v", err)
	}
	if _, err := db.Exec("INSERT INTO widgets (name) VALUES ('a')"); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	mustExec(t, bus, "widget.color", `{"id":1,"color":"red"}`)
	mustExec(t, bus, "widget.color", `{"id":1,"color":"green"}`)
	mustExec(t, bus, "widget.color", `{"id":1,"color":"blue"}`)

	if got := bus.History().UndoDepth; got != 2 {
		t.Fatalf("expected stack trimmed to limit 2, got %d", got)
	}

	// The oldest entry was evicted: one undo restores green, not red.
	if _, err := bus.Undo(); err != nil {
		t.Fatalf("undoing: %v", err)
	}
	var color string
	if err := db.QueryRow("SELECT color FROM widgets WHERE id = 1").Scan(&color); err != nil {
		t.Fatalf("reading color: %v", err)
	}
	if color != "green" {
		t.Errorf("expected evicted oldest entry, undo lands on green, got %q", color)
	}
}

func TestUndoSkipsStaleEntriesWithNotice(t *testing.T) {
	bus, db := newTestBus(t)
	if err := bus.Register(renameWidgetCommand()); err != nil {
		t.Fatalf("registering: %v", err)
	}
	if _, err := db.Exec("INSERT INTO widgets (name) VALUES ('one')"); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	if _, err := db.Exec("INSERT INTO widgets (name) VALUES ('two')"); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	mustExec(t, bus, "widget.rename", `{"id":1,"name":"one-renamed"}`)
	mustExec(t, bus, "widget.rename", `{"id":2,"name":"two-renamed"}`)

	// The newest entry's target is destroyed out-of-band (e.g. permanent
	// delete), making it stale.
	if _, err := db.Exec("DELETE FROM widgets WHERE id = 2"); err != nil {
		t.Fatalf("destroying target: %v", err)
	}

	result, err := bus.Undo()
	if err != nil {
		t.Fatalf("undoing: %v", err)
	}
	if len(result.Notices) != 1 {
		t.Errorf("expected one skip notice, got %v", result.Notices)
	}
	if result.Command != "widget.rename" || widgetName(t, db, 1) != "one" {
		t.Errorf("expected stale entry skipped and older entry applied")
	}
	if bus.History().UndoDepth != 0 {
		t.Errorf("expected empty undo stack after skip")
	}
}

func TestPurgeDropsEntriesReferencingDestroyedRows(t *testing.T) {
	bus, db := newTestBus(t)
	if err := bus.Register(renameWidgetCommand()); err != nil {
		t.Fatalf("registering: %v", err)
	}
	if _, err := db.Exec("INSERT INTO widgets (name) VALUES ('one')"); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	if _, err := db.Exec("INSERT INTO widgets (name) VALUES ('two')"); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	mustExec(t, bus, "widget.rename", `{"id":1,"name":"one-renamed"}`)
	mustExec(t, bus, "widget.rename", `{"id":2,"name":"two-renamed"}`)
	if _, err := bus.Undo(); err != nil {
		t.Fatalf("undoing: %v", err)
	}

	// Permanent delete of widget 1 purges every entry referencing it, from
	// both stacks. The rename of widget 2 does not reference widget 1 and
	// must survive.
	bus.Purge("widgets", 1)

	history := bus.History()
	if history.UndoDepth != 0 || history.RedoDepth != 1 {
		t.Fatalf("expected purge to drop only referencing entries, got undo=%d redo=%d", history.UndoDepth, history.RedoDepth)
	}

	bus.Purge("widgets", 2)
	history = bus.History()
	if history.UndoDepth != 0 || history.RedoDepth != 0 {
		t.Fatalf("expected purged stacks, got undo=%d redo=%d", history.UndoDepth, history.RedoDepth)
	}
	if _, err := bus.Undo(); !errors.Is(err, ErrNothingToUndo) {
		t.Errorf("expected ErrNothingToUndo after purge, got %v", err)
	}
}

func TestUndoOnEmptyStackReturnsErrNothingToUndo(t *testing.T) {
	bus, _ := newTestBus(t)
	if _, err := bus.Undo(); !errors.Is(err, ErrNothingToUndo) {
		t.Fatalf("expected ErrNothingToUndo, got %v", err)
	}
	if _, err := bus.Redo(); !errors.Is(err, ErrNothingToRedo) {
		t.Fatalf("expected ErrNothingToRedo, got %v", err)
	}
}

func TestUnknownCommandIsRejected(t *testing.T) {
	bus, _ := newTestBus(t)
	if _, err := bus.Execute("nope", json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected unknown command error")
	}
}
