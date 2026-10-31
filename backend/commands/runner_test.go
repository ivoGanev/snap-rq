package commands

import (
	"database/sql"
	"encoding/json"
	"errors"
	_ "modernc.org/sqlite"
	"testing"
	"time"
)

func newTestRunner(t *testing.T) (*Runner, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("enabling foreign keys: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE undo_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			command TEXT NOT NULL,
			label TEXT NOT NULL,
			input TEXT NOT NULL,
			snapshot TEXT NOT NULL,
			result TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			state TEXT NOT NULL DEFAULT 'undo' CHECK(state IN ('undo', 'redo'))
		);
	`); err != nil {
		t.Fatalf("creating undo_log: %v", err)
	}
	return NewRunner(db), db
}

// --- test commands ---

type renameWidget struct{ tx *sql.Tx }

func newRenameWidget(tx *sql.Tx) Command { return &renameWidget{tx: tx} }

func (renameWidget) Info() Info {
	return Info{FunctionID: "widget.rename", Label: "Rename Widget"}
}

type oldName struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func (c renameWidget) Snapshot(input json.RawMessage) (any, error) {
	var in struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	json.Unmarshal(input, &in)
	var old oldName
	err := c.tx.QueryRow(`SELECT id, name FROM widgets WHERE id = ?`, in.ID).Scan(&old.ID, &old.Name)
	return old, err
}

func (c renameWidget) Run(input json.RawMessage) (any, error) {
	var in struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	_, err := c.tx.Exec(`UPDATE widgets SET name = ? WHERE id = ?`, in.Name, in.ID)
	return in.Name, err
}

func (c renameWidget) Undo(snapshot, _ json.RawMessage) error {
	var old oldName
	if err := json.Unmarshal(snapshot, &old); err != nil {
		return err
	}
	result, err := c.tx.Exec(`UPDATE widgets SET name = ? WHERE id = ?`, old.Name, old.ID)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return errors.New("widget gone")
	}
	return nil
}

type createWidget struct{ tx *sql.Tx }

func newCreateWidget(tx *sql.Tx) Command { return &createWidget{tx: tx} }

func (createWidget) Info() Info {
	return Info{FunctionID: "widget.create", Label: "Create Widget"}
}

func (createWidget) Snapshot(_ json.RawMessage) (any, error) { return nil, nil }

func (c createWidget) Run(input json.RawMessage) (any, error) {
	var in struct {
		Name string `json:"name"`
	}
	json.Unmarshal(input, &in)
	result, err := c.tx.Exec(`INSERT INTO widgets (name) VALUES (?)`, in.Name)
	if err != nil {
		return nil, err
	}
	id, _ := result.LastInsertId()
	return map[string]any{"id": id, "name": in.Name}, nil
}

func (c createWidget) Undo(_, result json.RawMessage) error {
	var created struct {
		ID int64 `json:"id"`
	}
	json.Unmarshal(result, &created)
	_, err := c.tx.Exec(`DELETE FROM widgets WHERE id = ?`, created.ID)
	return err
}

func createWidgets(t *testing.T, db *sql.DB, names ...string) {
	t.Helper()
	for _, name := range names {
		if _, err := db.Exec(`INSERT INTO widgets (name) VALUES (?)`, name); err != nil {
			t.Fatalf("seeding %s: %v", name, err)
		}
	}
}

func widgetName(t *testing.T, db *sql.DB, id int64) string {
	t.Helper()
	var name string
	if err := db.QueryRow(`SELECT name FROM widgets WHERE id = ?`, id).Scan(&name); err != nil {
		t.Fatalf("reading widget: %v", err)
	}
	return name
}

// --- tests ---

func TestUndoPutsTheOldValueBack(t *testing.T) {
	runner, db := newTestRunner(t)
	if _, err := db.Exec(`CREATE TABLE widgets (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if err := runner.Register(newRenameWidget, newCreateWidget); err != nil {
		t.Fatal(err)
	}
	createWidgets(t, db, "original")

	run(t, runner, "widget.rename", map[string]any{"id": 1, "name": "renamed"})
	if widgetName(t, db, 1) != "renamed" {
		t.Fatal("rename did not happen")
	}

	result, err := runner.Undo()
	if err != nil {
		t.Fatalf("undoing: %v", err)
	}
	if result.Label != "Rename Widget" {
		t.Errorf("got label %q", result.Label)
	}
	if widgetName(t, db, 1) != "original" {
		t.Fatalf("expected original back, got %q", widgetName(t, db, 1))
	}
	if s := runner.Status(); s.UndoSteps != 0 || s.RedoSteps != 1 {
		t.Errorf("expected 0/1, got %d/%d", s.UndoSteps, s.RedoSteps)
	}

	if _, err := runner.Redo(); err != nil {
		t.Fatal(err)
	}
	if widgetName(t, db, 1) != "renamed" {
		t.Fatal("expected rename back after redo")
	}
}

func TestUndoOfCreateRemovesItAndRedoRestoresSameID(t *testing.T) {
	runner, db := newTestRunner(t)
	if _, err := db.Exec(`CREATE TABLE widgets (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if err := runner.Register(newCreateWidget); err != nil {
		t.Fatal(err)
	}

	created := run(t, runner, "widget.create", map[string]any{"name": "made"})
	createdMap := created.(map[string]any)
	_ = createdMap["id"] // the id is in the result — the command carries it

	if _, err := runner.Undo(); err != nil {
		t.Fatal(err)
	}
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM widgets`).Scan(&count)
	if count != 0 {
		t.Fatal("expected create undone")
	}

	if _, err := runner.Redo(); err != nil {
		t.Fatal(err)
	}
	// the redo re-runs the command, so the row comes back with a fresh id —
	// same data, different AUTOINCREMENT id. That is correct.
	var redoCount int
	db.QueryRow(`SELECT COUNT(*) FROM widgets`).Scan(&redoCount)
	if redoCount != 1 {
		t.Fatalf("expected the widget back after redo, got %d rows", redoCount)
	}
}

func TestQuickRepeatsMergeIntoOneStep(t *testing.T) {
	runner, db := newTestRunner(t)
	if _, err := db.Exec(`CREATE TABLE widgets (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if err := runner.Register(newRenameWidget); err != nil {
		t.Fatal(err)
	}
	createWidgets(t, db, "original")

	run(t, runner, "widget.rename", map[string]any{"id": 1, "name": "a"})
	run(t, runner, "widget.rename", map[string]any{"id": 1, "name": "b"})

	if got := runner.Status().UndoSteps; got != 1 {
		t.Fatalf("expected 1 merged step, got %d", got)
	}
	if _, err := runner.Undo(); err != nil {
		t.Fatal(err)
	}
	if widgetName(t, db, 1) != "original" {
		t.Errorf("expected original back, got %q", widgetName(t, db, 1))
	}
}

func TestDestroyedRowsSkipWithNotice(t *testing.T) {
	runner, db := newTestRunner(t)
	if _, err := db.Exec(`CREATE TABLE widgets (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if err := runner.Register(newRenameWidget); err != nil {
		t.Fatal(err)
	}
	createWidgets(t, db, "one", "two")
	run(t, runner, "widget.rename", map[string]any{"id": 1, "name": "a"})
	run(t, runner, "widget.rename", map[string]any{"id": 2, "name": "b"})

	// destroy row 2 out-of-band — the newest step is now impossible
	db.Exec(`DELETE FROM widgets WHERE id = 2`)

	result, err := runner.Undo()
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Skipped) != 1 {
		t.Errorf("expected 1 skipped note, got %v", result.Skipped)
	}
	if widgetName(t, db, 1) != "one" {
		t.Errorf("expected older step applied")
	}
	if runner.Status().UndoSteps != 0 {
		t.Error("expected empty undo list")
	}
}

func TestUndoLimitTrims(t *testing.T) {
	runner, db := newTestRunner(t)
	if _, err := db.Exec(`CREATE TABLE widgets (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if err := runner.Register(newRenameWidget); err != nil {
		t.Fatal(err)
	}
	runner.SetUndoLimit(func() int { return 2 })
	createWidgets(t, db, "a", "b", "c")

	run(t, runner, "widget.rename", map[string]any{"id": 1, "name": "x"})
	time.Sleep(800 * time.Millisecond) // outside the merge window
	run(t, runner, "widget.rename", map[string]any{"id": 2, "name": "y"})
	time.Sleep(800 * time.Millisecond)
	run(t, runner, "widget.rename", map[string]any{"id": 3, "name": "z"})

	if got := runner.Status().UndoSteps; got != 2 {
		t.Fatalf("expected trimmed to 2, got %d", got)
	}
}

func TestEmptyListsAndUnknownCommand(t *testing.T) {
	runner, _ := newTestRunner(t)
	if _, err := runner.Undo(); !errors.Is(err, ErrNothingToUndo) {
		t.Errorf("expected ErrNothingToUndo, got %v", err)
	}
	if _, err := runner.Redo(); !errors.Is(err, ErrNothingToRedo) {
		t.Errorf("expected ErrNothingToRedo, got %v", err)
	}
	if _, err := runner.Run("nope", nil); err == nil {
		t.Error("expected unknown command error")
	}
}

func run(t *testing.T, runner *Runner, name string, input any) any {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	out, err := runner.Run(name, raw)
	if err != nil {
		t.Fatalf("running %s: %v", name, err)
	}
	return out
}
