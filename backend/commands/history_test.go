package commands

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	_ "modernc.org/sqlite"
)

func newTestHistory(t *testing.T) (*History, *sql.DB) {
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
	return NewHistory(db), db
}

func countWidgets(t *testing.T, db *sql.DB) int {
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

// renameWidget renames a widget, the way a user command would: it saves a
// copy of the row, changes it, and notes what the user will see.
func renameWidget() *Command {
	return &Command{
		Name:     "widget.rename",
		Label:    "Rename Widget",
		Undoable: true,
		Apply: func(tx *sql.Tx, rec *Recorder, input json.RawMessage) (any, error) {
			var payload struct {
				ID   int64  `json:"id"`
				Name string `json:"name"`
			}
			if err := json.Unmarshal(input, &payload); err != nil {
				return nil, err
			}
			if err := rec.SaveRows("widgets", payload.ID); err != nil {
				return nil, err
			}
			if _, err := tx.Exec("UPDATE widgets SET name = ? WHERE id = ?", payload.Name, payload.ID); err != nil {
				return nil, err
			}
			rec.Touched("widget", "update", payload.ID)
			return payload.Name, nil
		},
	}
}

func run(t *testing.T, history *History, name string, input string) any {
	t.Helper()
	result, err := history.Run(name, json.RawMessage(input))
	if err != nil {
		t.Fatalf("running %s: %v", name, err)
	}
	return result
}

func TestUndoPutsTheOldRowBack(t *testing.T) {
	history, db := newTestHistory(t)
	if err := history.Register(renameWidget()); err != nil {
		t.Fatalf("registering: %v", err)
	}
	if _, err := db.Exec("INSERT INTO widgets (name) VALUES ('original')"); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	run(t, history, "widget.rename", `{"id":1,"name":"renamed"}`)
	if widgetName(t, db, 1) != "renamed" {
		t.Fatalf("rename did not happen")
	}

	result, err := history.Undo()
	if err != nil {
		t.Fatalf("undoing: %v", err)
	}
	if result.Label != "Rename Widget" {
		t.Errorf("expected the label in the undo result, got %q", result.Label)
	}
	if widgetName(t, db, 1) != "original" {
		t.Fatalf("expected the original name back, got %q", widgetName(t, db, 1))
	}
	if history.Status().UndoSteps != 0 || history.Status().RedoSteps != 1 {
		t.Errorf("expected 0 undo / 1 redo steps, got %d/%d",
			history.Status().UndoSteps, history.Status().RedoSteps)
	}

	if _, err := history.Redo(); err != nil {
		t.Fatalf("redoing: %v", err)
	}
	if widgetName(t, db, 1) != "renamed" {
		t.Fatalf("expected the rename back after redo, got %q", widgetName(t, db, 1))
	}
}

// The most important test in this file: undoing a rename must not wipe out
// a status update written by something else in the meantime (a request
// send). Only the columns the command changed go back.
func TestUndoKeepsColumnsSomeoneElseChanged(t *testing.T) {
	history, db := newTestHistory(t)
	if err := history.Register(renameWidget()); err != nil {
		t.Fatalf("registering: %v", err)
	}
	if _, err := db.Exec("INSERT INTO widgets (name) VALUES ('original')"); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	run(t, history, "widget.rename", `{"id":1,"name":"renamed"}`)

	// A send happens in between and writes the status code.
	if _, err := db.Exec("UPDATE widgets SET status = 500 WHERE id = 1"); err != nil {
		t.Fatalf("writing the status: %v", err)
	}

	if _, err := history.Undo(); err != nil {
		t.Fatalf("undoing: %v", err)
	}

	if widgetName(t, db, 1) != "original" {
		t.Errorf("expected the name restored, got %q", widgetName(t, db, 1))
	}
	if got := widgetStatus(t, db, 1); got != 500 {
		t.Errorf("expected the status written in between to survive, got %d", got)
	}
}

func TestUndoOfCreateDeletesItAgainAndRedoBringsItBackWithSameID(t *testing.T) {
	history, db := newTestHistory(t)
	if err := history.Register(&Command{
		Name:     "widget.create",
		Label:    "Create Widget",
		Undoable: true,
		Apply: func(tx *sql.Tx, rec *Recorder, input json.RawMessage) (any, error) {
			var payload struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(input, &payload); err != nil {
				return nil, err
			}
			result, err := tx.Exec("INSERT INTO widgets (name) VALUES (?)", payload.Name)
			if err != nil {
				return nil, err
			}
			id, err := result.LastInsertId()
			if err != nil {
				return nil, err
			}
			if err := rec.SaveNewRows("widgets", id); err != nil {
				return nil, err
			}
			rec.Touched("widget", "create", id)
			return id, nil
		},
	}); err != nil {
		t.Fatalf("registering: %v", err)
	}

	created := run(t, history, "widget.create", `{"name":"made"}`)
	createdID, ok := created.(int64)
	if !ok {
		t.Fatalf("expected an int64 result, got %T", created)
	}
	if countWidgets(t, db) != 1 {
		t.Fatalf("expected 1 widget after create")
	}

	if _, err := history.Undo(); err != nil {
		t.Fatalf("undoing: %v", err)
	}
	if countWidgets(t, db) != 0 {
		t.Fatalf("expected the create undone")
	}

	if _, err := history.Redo(); err != nil {
		t.Fatalf("redoing: %v", err)
	}
	if countWidgets(t, db) != 1 {
		t.Fatalf("expected the widget back after redo")
	}
	var redoneID int64
	if err := db.QueryRow("SELECT id FROM widgets").Scan(&redoneID); err != nil {
		t.Fatalf("reading the redone widget: %v", err)
	}
	if redoneID != createdID {
		t.Errorf("expected redo to bring back id %d, got %d", createdID, redoneID)
	}
}

func TestUndoOfDeletePutsTheRowBackAsItWas(t *testing.T) {
	history, db := newTestHistory(t)
	if err := history.Register(&Command{
		Name:     "widget.delete",
		Label:    "Delete Widget",
		Undoable: true,
		Apply: func(tx *sql.Tx, rec *Recorder, input json.RawMessage) (any, error) {
			var payload struct {
				ID int64 `json:"id"`
			}
			if err := json.Unmarshal(input, &payload); err != nil {
				return nil, err
			}
			if err := rec.SaveRows("widgets", payload.ID); err != nil {
				return nil, err
			}
			if _, err := tx.Exec("DELETE FROM widgets WHERE id = ?", payload.ID); err != nil {
				return nil, err
			}
			rec.Touched("widget", "delete", payload.ID)
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

	run(t, history, "widget.delete", fmt.Sprintf(`{"id":%d}`, id))
	if countWidgets(t, db) != 0 {
		t.Fatalf("expected the delete to happen")
	}

	if _, err := history.Undo(); err != nil {
		t.Fatalf("undoing: %v", err)
	}
	if countWidgets(t, db) != 1 {
		t.Fatalf("expected the widget back")
	}
	var restoredID int64
	var name, color string
	if err := db.QueryRow("SELECT id, name, color FROM widgets").Scan(&restoredID, &name, &color); err != nil {
		t.Fatalf("reading the restored widget: %v", err)
	}
	if restoredID != id || name != "keep-me" || color != "red" {
		t.Errorf("expected the original row back, got id=%d name=%q color=%q", restoredID, name, color)
	}
}

func TestQuickRepeatsMergeIntoOneStep(t *testing.T) {
	history, db := newTestHistory(t)
	if err := history.Register(renameWidget()); err != nil {
		t.Fatalf("registering: %v", err)
	}
	if _, err := db.Exec("INSERT INTO widgets (name) VALUES ('original')"); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	run(t, history, "widget.rename", `{"id":1,"name":"a"}`)
	run(t, history, "widget.rename", `{"id":1,"name":"b"}`)

	if got := history.Status().UndoSteps; got != 1 {
		t.Fatalf("expected the two renames to be one step, got %d", got)
	}

	// One undo must land where the typing started, not in the middle.
	if _, err := history.Undo(); err != nil {
		t.Fatalf("undoing: %v", err)
	}
	if widgetName(t, db, 1) != "original" {
		t.Errorf("expected the original name back, got %q", widgetName(t, db, 1))
	}
}

func TestNewUndoableCommandMakesRedoImpossible(t *testing.T) {
	history, db := newTestHistory(t)
	if err := history.Register(renameWidget()); err != nil {
		t.Fatalf("registering: %v", err)
	}
	if _, err := db.Exec("INSERT INTO widgets (name) VALUES ('original')"); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	if _, err := db.Exec("INSERT INTO widgets (name) VALUES ('other')"); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	run(t, history, "widget.rename", `{"id":1,"name":"a"}`)
	if _, err := history.Undo(); err != nil {
		t.Fatalf("undoing: %v", err)
	}
	if history.Status().RedoSteps != 1 {
		t.Fatalf("expected 1 redo step")
	}

	// Different widget, so this cannot merge with anything.
	run(t, history, "widget.rename", `{"id":2,"name":"b"}`)

	if got := history.Status().RedoSteps; got != 0 {
		t.Errorf("expected the redo steps gone, got %d", got)
	}
}

func TestChangesThatCannotBeUndoneNeverTouchTheLists(t *testing.T) {
	history, db := newTestHistory(t)
	if err := history.Register(&Command{
		Name:     "widget.status",
		Label:    "Set Widget Status",
		Undoable: false,
		Apply: func(tx *sql.Tx, rec *Recorder, input json.RawMessage) (any, error) {
			var payload struct {
				ID     int64 `json:"id"`
				Status int   `json:"status"`
			}
			if err := json.Unmarshal(input, &payload); err != nil {
				return nil, err
			}
			if _, err := tx.Exec("UPDATE widgets SET status = ? WHERE id = ?", payload.Status, payload.ID); err != nil {
				return nil, err
			}
			return nil, nil
		},
	}); err != nil {
		t.Fatalf("registering: %v", err)
	}
	if err := history.Register(renameWidget()); err != nil {
		t.Fatalf("registering: %v", err)
	}
	if _, err := db.Exec("INSERT INTO widgets (name) VALUES ('original')"); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	run(t, history, "widget.rename", `{"id":1,"name":"renamed"}`)
	if _, err := history.Undo(); err != nil {
		t.Fatalf("undoing: %v", err)
	}

	// A send happens between the undo and the redo: it must not wipe out
	// the redo step.
	run(t, history, "widget.status", `{"id":1,"status":500}`)

	status := history.Status()
	if status.RedoSteps != 1 {
		t.Errorf("expected the redo step to survive, got %d", status.RedoSteps)
	}
	if status.UndoSteps != 0 {
		t.Errorf("expected no undo steps, got %d", status.UndoSteps)
	}
	if _, err := history.Redo(); err != nil {
		t.Fatalf("expected redo to still work: %v", err)
	}
}

func TestUndoListShrinksToTheLimitAndDropsTheOldest(t *testing.T) {
	history, db := newTestHistory(t)
	history.SetUndoLimit(func() int { return 2 })
	if err := history.Register(&Command{
		Name:     "widget.color",
		Label:    "Colour Widget",
		Undoable: true,
		Apply: func(tx *sql.Tx, rec *Recorder, input json.RawMessage) (any, error) {
			var payload struct {
				ID    int64  `json:"id"`
				Color string `json:"color"`
			}
			if err := json.Unmarshal(input, &payload); err != nil {
				return nil, err
			}
			if err := rec.SaveRows("widgets", payload.ID); err != nil {
				return nil, err
			}
			if _, err := tx.Exec("UPDATE widgets SET color = ? WHERE id = ?", payload.Color, payload.ID); err != nil {
				return nil, err
			}
			return nil, nil
		},
	}); err != nil {
		t.Fatalf("registering: %v", err)
	}
	// Three different widgets, so the three colourings are three steps
	// (quick repeats on the SAME row would merge into one).
	if _, err := db.Exec("INSERT INTO widgets (name) VALUES ('a'), ('b'), ('c')"); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	run(t, history, "widget.color", `{"id":1,"color":"red"}`)
	run(t, history, "widget.color", `{"id":2,"color":"green"}`)
	run(t, history, "widget.color", `{"id":3,"color":"blue"}`)

	if got := history.Status().UndoSteps; got != 2 {
		t.Fatalf("expected the list trimmed to 2, got %d", got)
	}

	// The oldest step was dropped, so undoing takes widget 3's colour
	// away; widget 1's dropped step is simply gone.
	if _, err := history.Undo(); err != nil {
		t.Fatalf("undoing: %v", err)
	}
	var color sql.NullString
	if err := db.QueryRow("SELECT color FROM widgets WHERE id = 3").Scan(&color); err != nil {
		t.Fatalf("reading the color: %v", err)
	}
	if color.Valid && color.String != "" {
		t.Errorf("expected widget 3's colour gone, got %q", color.String)
	}
}

func TestQuickRepeatsOnDifferentRowsDoNotMerge(t *testing.T) {
	history, db := newTestHistory(t)
	if err := history.Register(renameWidget()); err != nil {
		t.Fatalf("registering: %v", err)
	}
	if _, err := db.Exec("INSERT INTO widgets (name) VALUES ('one'), ('two')"); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	run(t, history, "widget.rename", `{"id":1,"name":"a"}`)
	run(t, history, "widget.rename", `{"id":2,"name":"b"}`)

	if got := history.Status().UndoSteps; got != 2 {
		t.Fatalf("expected two separate steps for two different rows, got %d", got)
	}
}

func TestUndoSkipsStepsWhoseRowsAreGoneForGood(t *testing.T) {
	history, db := newTestHistory(t)
	if err := history.Register(renameWidget()); err != nil {
		t.Fatalf("registering: %v", err)
	}
	if _, err := db.Exec("INSERT INTO widgets (name) VALUES ('one')"); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	if _, err := db.Exec("INSERT INTO widgets (name) VALUES ('two')"); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	run(t, history, "widget.rename", `{"id":1,"name":"one-renamed"}`)
	run(t, history, "widget.rename", `{"id":2,"name":"two-renamed"}`)

	// The newest step's row is destroyed for good, making it impossible.
	if _, err := db.Exec("DELETE FROM widgets WHERE id = 2"); err != nil {
		t.Fatalf("destroying the row: %v", err)
	}

	result, err := history.Undo()
	if err != nil {
		t.Fatalf("undoing: %v", err)
	}
	if len(result.Skipped) != 1 {
		t.Errorf("expected one skipped note, got %v", result.Skipped)
	}
	if result.Command != "widget.rename" || widgetName(t, db, 1) != "one" {
		t.Errorf("expected the impossible step skipped and the older one applied")
	}
	if history.Status().UndoSteps != 0 {
		t.Errorf("expected an empty undo list after the skip")
	}
}

func TestForgetRowsDropsEveryStepThatInvolvedThem(t *testing.T) {
	history, db := newTestHistory(t)
	if err := history.Register(renameWidget()); err != nil {
		t.Fatalf("registering: %v", err)
	}
	if _, err := db.Exec("INSERT INTO widgets (name) VALUES ('one')"); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	if _, err := db.Exec("INSERT INTO widgets (name) VALUES ('two')"); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	run(t, history, "widget.rename", `{"id":1,"name":"one-renamed"}`)
	run(t, history, "widget.rename", `{"id":2,"name":"two-renamed"}`)
	if _, err := history.Undo(); err != nil {
		t.Fatalf("undoing: %v", err)
	}

	// Widget 1 is destroyed for good: every step that involved it goes.
	history.ForgetRows("widgets", 1)

	status := history.Status()
	if status.UndoSteps != 0 || status.RedoSteps != 1 {
		t.Fatalf("expected only the widget-2 redo step to survive, got undo=%d redo=%d",
			status.UndoSteps, status.RedoSteps)
	}

	history.ForgetRows("widgets", 2)
	status = history.Status()
	if status.UndoSteps != 0 || status.RedoSteps != 0 {
		t.Fatalf("expected empty lists, got undo=%d redo=%d", status.UndoSteps, status.RedoSteps)
	}
	if _, err := history.Undo(); !errors.Is(err, ErrNothingToUndo) {
		t.Errorf("expected ErrNothingToUndo, got %v", err)
	}
}

func TestUndoAndRedoOnEmptyLists(t *testing.T) {
	history, _ := newTestHistory(t)
	if _, err := history.Undo(); !errors.Is(err, ErrNothingToUndo) {
		t.Fatalf("expected ErrNothingToUndo, got %v", err)
	}
	if _, err := history.Redo(); !errors.Is(err, ErrNothingToRedo) {
		t.Fatalf("expected ErrNothingToRedo, got %v", err)
	}
}

func TestRunOfUnknownNameFails(t *testing.T) {
	history, _ := newTestHistory(t)
	if _, err := history.Run("nope", json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected an unknown-command error")
	}
}

func TestPlanRestore(t *testing.T) {
	row := func(table string, id int64, columns map[string]any) SavedRow {
		return SavedRow{Table: table, ID: id, Columns: columns}
	}

	before := []SavedRow{
		row("widgets", 1, map[string]any{"id": int64(1), "name": "old", "status": int64(0)}),
		row("widgets", 2, map[string]any{"id": int64(2), "name": "gone"}),
	}
	after := []SavedRow{
		row("widgets", 1, map[string]any{"id": int64(1), "name": "new", "status": int64(0)}),
		row("widgets", 3, map[string]any{"id": int64(3), "name": "made"}),
	}

	plan := planRestore(before, after)

	if len(plan.updates) != 1 {
		t.Fatalf("expected one update, got %d", len(plan.updates))
	}
	update := plan.updates[0]
	if update.id != 1 || len(update.columns) != 1 {
		t.Fatalf("expected only the name column on row 1, got %v", update.columns)
	}
	if update.columns["name"] != "old" {
		t.Errorf("expected the old name back, got %v", update.columns["name"])
	}

	if len(plan.inserts) != 1 || plan.inserts[0].ID != 2 {
		t.Fatalf("expected row 2 inserted back, got %v", plan.inserts)
	}
	if len(plan.deletes) != 1 || plan.deletes[0].ID != 3 {
		t.Fatalf("expected row 3 deleted again, got %v", plan.deletes)
	}
}

func TestChangedColumnsTreatsMissingColumnsAsChanged(t *testing.T) {
	// A schema migration added "color" after the step was remembered: the
	// older copy has no opinion on it, so the saved value is written back.
	row := SavedRow{Table: "widgets", ID: 1, Columns: map[string]any{
		"id": int64(1), "name": "same", "color": "red",
	}}
	counterpart := SavedRow{Table: "widgets", ID: 1, Columns: map[string]any{
		"id": int64(1), "name": "same",
	}}

	changed := changedColumns(row, counterpart)
	if len(changed) != 1 || changed["color"] != "red" {
		t.Fatalf("expected color to count as changed, got %v", changed)
	}
}
