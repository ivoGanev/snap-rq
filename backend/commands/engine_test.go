package commands

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	_ "modernc.org/sqlite"
	"snap-rq/backend/store"
)

func newTestEngine(t *testing.T) (*Engine, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening in-memory db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("enabling foreign keys: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE widgets (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			color TEXT,
			status INTEGER NOT NULL DEFAULT 0,
			sort_order INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE widget_parts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			widget_id INTEGER NOT NULL,
			label TEXT NOT NULL,
			FOREIGN KEY (widget_id) REFERENCES widgets(id) ON DELETE CASCADE
		);
	`); err != nil {
		t.Fatalf("creating test schema: %v", err)
	}

	manifest := &Manifest{Tables: map[string]TableRule{
		"widgets": {Entity: "widget", Children: map[string]ChildRule{
			"widget_parts": {},
		}},
		"widget_parts": {Entity: "part"},
	}}
	engine := NewEngineWithManifest(db, manifest)
	if err := engine.Start(); err != nil {
		t.Fatalf("starting engine: %v", err)
	}
	return engine, db
}

func mustRun(t *testing.T, engine *Engine, name string, input any) any {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("encoding input: %v", err)
	}
	out, err := engine.Run(name, raw)
	if err != nil {
		t.Fatalf("running %s: %v", name, err)
	}
	return out
}

func widgetName(t *testing.T, db *sql.DB, id int64) string {
	t.Helper()
	var name string
	if err := db.QueryRow("SELECT name FROM widgets WHERE id = ?", id).Scan(&name); err != nil {
		t.Fatalf("reading widget %d: %v", id, err)
	}
	return name
}

func count(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("counting: %v", err)
	}
	return n
}

// ---------------------------------------------------------------------------
// Commands used by the tests
// ---------------------------------------------------------------------------

type renameWidget struct{}

func (renameWidget) Info() Info {
	return Info{
		FunctionID:      "widget.rename",
		Label:           "Rename Widget",
		UndoOperationID: "widget.rename",
		Columns:         []string{"name"},
	}
}

func (renameWidget) Run(db store.Session, input json.RawMessage) (any, error) {
	var payload struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, err
	}
	if err := db.Update("widgets", payload.ID, map[string]any{"name": payload.Name}); err != nil {
		return nil, err
	}
	return payload.Name, nil
}

type createWidget struct{}

func (createWidget) Info() Info {
	return Info{
		FunctionID:      "widget.create",
		Label:           "Create Widget",
		UndoOperationID: "widget.delete",
	}
}

func (createWidget) Run(db store.Session, input json.RawMessage) (any, error) {
	var payload struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, err
	}
	return db.Insert("widgets", map[string]any{"name": payload.Name})
}

type deleteWidget struct{}

func (deleteWidget) Info() Info {
	return Info{
		FunctionID:      "widget.delete",
		Label:           "Delete Widget",
		UndoOperationID: "widget.create",
	}
}

func (deleteWidget) Run(db store.Session, input json.RawMessage) (any, error) {
	var payload struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, err
	}
	return nil, db.Delete("widgets", payload.ID)
}

type setStatus struct{}

func (setStatus) Info() Info {
	// A send-style command: writes a column the user's commands never own,
	// and cannot be undone.
	return Info{FunctionID: "widget.set_status", Label: "Set Status", Columns: []string{"status"}}
}

func (setStatus) Run(db store.Session, input json.RawMessage) (any, error) {
	var payload struct {
		ID     int64 `json:"id"`
		Status int   `json:"status"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, err
	}
	return nil, db.Update("widgets", payload.ID, map[string]any{"status": payload.Status})
}

type listWidgets struct{}

func (listWidgets) Info() Info {
	return Info{FunctionID: "widget.list", Label: "List Widgets", ReadOnly: true}
}

func (listWidgets) Run(db store.Session, _ json.RawMessage) (any, error) {
	rows, err := db.Query("SELECT id, name FROM widgets ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var widgets []map[string]any
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		widgets = append(widgets, map[string]any{"id": id, "name": name})
	}
	return widgets, rows.Err()
}

func registerAll(t *testing.T, engine *Engine) {
	t.Helper()
	if err := engine.Register(
		renameWidget{}, createWidget{}, deleteWidget{}, setStatus{}, listWidgets{},
	); err != nil {
		t.Fatalf("registering: %v", err)
	}
	if err := engine.Start(); err != nil {
		t.Fatalf("starting: %v", err)
	}
}

// ---------------------------------------------------------------------------
// The tests
// ---------------------------------------------------------------------------

func TestUndoPutsOldValuesBack(t *testing.T) {
	engine, db := newTestEngine(t)
	registerAll(t, engine)
	if _, err := db.Exec("INSERT INTO widgets (name) VALUES ('original')"); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	mustRun(t, engine, "widget.rename", map[string]any{"id": 1, "name": "renamed"})
	if widgetName(t, db, 1) != "renamed" {
		t.Fatalf("rename did not happen")
	}

	result, err := engine.Undo()
	if err != nil {
		t.Fatalf("undoing: %v", err)
	}
	if result.Label != "Rename Widget" {
		t.Errorf("expected the label in the undo result, got %q", result.Label)
	}
	if widgetName(t, db, 1) != "original" {
		t.Fatalf("expected the original name back, got %q", widgetName(t, db, 1))
	}
	if engine.Status().UndoSteps != 0 || engine.Status().RedoSteps != 1 {
		t.Errorf("expected 0 undo / 1 redo steps, got %d/%d",
			engine.Status().UndoSteps, engine.Status().RedoSteps)
	}

	if _, err := engine.Redo(); err != nil {
		t.Fatalf("redoing: %v", err)
	}
	if widgetName(t, db, 1) != "renamed" {
		t.Fatalf("expected the rename back after redo")
	}
}

// The most important test in this file: undoing a rename must not wipe out
// a status written by something else in the meantime (a request send).
// Only the columns the command declared go back.
func TestUndoKeepsColumnsSomeoneElseChanged(t *testing.T) {
	engine, db := newTestEngine(t)
	registerAll(t, engine)
	mustRun(t, engine, "widget.create", map[string]any{"name": "original"})
	mustRun(t, engine, "widget.rename", map[string]any{"id": 1, "name": "renamed"})

	// A send happens in between and writes the status.
	mustRun(t, engine, "widget.set_status", map[string]any{"id": 1, "status": 500})

	if _, err := engine.Undo(); err != nil {
		t.Fatalf("undoing: %v", err)
	}

	if widgetName(t, db, 1) != "original" {
		t.Errorf("expected the name restored, got %q", widgetName(t, db, 1))
	}
	var status int
	if err := db.QueryRow("SELECT status FROM widgets WHERE id = 1").Scan(&status); err != nil {
		t.Fatalf("reading status: %v", err)
	}
	if status != 500 {
		t.Errorf("expected the status written in between to survive, got %d", status)
	}
}

func TestUndoOfCreateDeletesItAgainAndRedoBringsItBackWithSameID(t *testing.T) {
	engine, db := newTestEngine(t)
	registerAll(t, engine)

	created := mustRun(t, engine, "widget.create", map[string]any{"name": "made"})
	id, ok := created.(int64)
	if !ok {
		t.Fatalf("expected an int64 back, got %T", created)
	}

	if _, err := engine.Undo(); err != nil {
		t.Fatalf("undoing: %v", err)
	}
	if count(t, db, "SELECT COUNT(*) FROM widgets") != 0 {
		t.Fatalf("expected the create undone")
	}

	if _, err := engine.Redo(); err != nil {
		t.Fatalf("redoing: %v", err)
	}
	if count(t, db, "SELECT COUNT(*) FROM widgets WHERE id = ?", id) != 1 {
		t.Fatalf("expected the widget back under its original id %d", id)
	}
}

func TestUndoOfDeleteRestoresTheCascadeTree(t *testing.T) {
	engine, db := newTestEngine(t)
	registerAll(t, engine)
	mustRun(t, engine, "widget.create", map[string]any{"name": "victim"})

	// Give the widget a child part.
	if _, err := db.Exec(
		`INSERT INTO widget_parts (widget_id, label) VALUES (1, 'arm')`); err != nil {
		t.Fatalf("seeding a part: %v", err)
	}

	mustRun(t, engine, "widget.delete", map[string]any{"id": 1})
	if count(t, db, "SELECT COUNT(*) FROM widgets") != 0 {
		t.Fatalf("expected the delete to happen")
	}
	if count(t, db, "SELECT COUNT(*) FROM widget_parts") != 0 {
		t.Fatalf("expected the cascade to remove the part")
	}

	if _, err := engine.Undo(); err != nil {
		t.Fatalf("undoing: %v", err)
	}
	if count(t, db, "SELECT COUNT(*) FROM widgets WHERE id = 1") != 1 {
		t.Fatalf("expected the widget back under its original id")
	}
	if count(t, db, "SELECT COUNT(*) FROM widget_parts WHERE widget_id = 1") != 1 {
		t.Fatalf("expected the cascade part back under its original parent")
	}
}

func TestQuickRepeatsMergeIntoOneStep(t *testing.T) {
	engine, db := newTestEngine(t)
	registerAll(t, engine)
	if _, err := db.Exec("INSERT INTO widgets (name) VALUES ('original')"); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	mustRun(t, engine, "widget.rename", map[string]any{"id": 1, "name": "a"})
	mustRun(t, engine, "widget.rename", map[string]any{"id": 1, "name": "b"})

	if got := engine.Status().UndoSteps; got != 1 {
		t.Fatalf("expected the two renames to be one step, got %d", got)
	}

	// One undo goes back to where the typing started.
	if _, err := engine.Undo(); err != nil {
		t.Fatalf("undoing: %v", err)
	}
	if widgetName(t, db, 1) != "original" {
		t.Errorf("expected the original name back, got %q", widgetName(t, db, 1))
	}
}

func TestRepeatsOnDifferentRowsDoNotMerge(t *testing.T) {
	engine, _ := newTestEngine(t)
	registerAll(t, engine)
	if _, err := engine.DB().Exec("INSERT INTO widgets (name) VALUES ('one'), ('two')"); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	mustRun(t, engine, "widget.rename", map[string]any{"id": 1, "name": "a"})
	mustRun(t, engine, "widget.rename", map[string]any{"id": 2, "name": "b"})

	if got := engine.Status().UndoSteps; got != 2 {
		t.Fatalf("expected two separate steps for two different rows, got %d", got)
	}
}

func TestNewUndoableStepMakesRedoImpossible(t *testing.T) {
	engine, _ := newTestEngine(t)
	registerAll(t, engine)
	mustRun(t, engine, "widget.create", map[string]any{"name": "one"})
	mustRun(t, engine, "widget.rename", map[string]any{"id": 1, "name": "a"})
	if _, err := engine.Undo(); err != nil {
		t.Fatalf("undoing: %v", err)
	}
	if engine.Status().RedoSteps != 1 {
		t.Fatalf("expected 1 redo step")
	}

	mustRun(t, engine, "widget.rename", map[string]any{"id": 1, "name": "b"})
	if got := engine.Status().RedoSteps; got != 0 {
		t.Errorf("expected the redo steps gone, got %d", got)
	}
}

func TestStepsThatCannotBeUndoneNeverTouchTheLists(t *testing.T) {
	engine, _ := newTestEngine(t)
	registerAll(t, engine)
	mustRun(t, engine, "widget.create", map[string]any{"name": "one"})
	mustRun(t, engine, "widget.rename", map[string]any{"id": 1, "name": "a"})
	if _, err := engine.Undo(); err != nil {
		t.Fatalf("undoing: %v", err)
	}

	// A send-style write between the undo and the redo: it must not wipe
	// out the redo step.
	mustRun(t, engine, "widget.set_status", map[string]any{"id": 1, "status": 500})

	status := engine.Status()
	if status.RedoSteps != 1 {
		t.Errorf("expected the redo step to survive, got %d", status.RedoSteps)
	}
	if status.UndoSteps != 1 {
		t.Errorf("expected only the create step on the undo list, got %d", status.UndoSteps)
	}
	if _, err := engine.Redo(); err != nil {
		t.Fatalf("expected redo to still work: %v", err)
	}
}

func TestReadOnlyCommandsRunLockFreeAndCannotWrite(t *testing.T) {
	engine, db := newTestEngine(t)
	registerAll(t, engine)
	mustRun(t, engine, "widget.create", map[string]any{"name": "one"})

	widgets := mustRun(t, engine, "widget.list", nil)
	if list, ok := widgets.([]map[string]any); !ok || len(list) != 1 {
		t.Fatalf("expected the list to read one widget, got %v", widgets)
	}

	// A ReadOnly command attempting a write is a programming error caught
	// immediately.
	if _, err := db.Exec(`CREATE TABLE temptable (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("creating a helper table: %v", err)
	}
	engine.Register(readonlyWriter{})
	if err := engine.Start(); err != nil {
		t.Fatalf("restarting: %v", err)
	}
	if _, err := engine.Run("widget.illegal_write", nil); err == nil {
		t.Fatal("expected a ReadOnly write attempt to fail")
	}
}

type readonlyWriter struct{}

func (readonlyWriter) Info() Info {
	return Info{FunctionID: "widget.illegal_write", Label: "Illegal", ReadOnly: true}
}

func (readonlyWriter) Run(db store.Session, _ json.RawMessage) (any, error) {
	_, err := db.Insert("temptable", map[string]any{"id": 1})
	return nil, err
}

func TestColumnOwnershipIsEnforced(t *testing.T) {
	engine, _ := newTestEngine(t)
	registerAll(t, engine)
	mustRun(t, engine, "widget.create", map[string]any{"name": "one"})

	// renameWidget declares only "name"; writing status through it must
	// fail, so an undo can never touch a send's column.
	if err := engine.Register(illegalColumnWriter{}); err != nil {
		t.Fatalf("registering: %v", err)
	}
	if err := engine.Start(); err != nil {
		t.Fatalf("restarting: %v", err)
	}
	input, _ := json.Marshal(map[string]any{"id": 1, "status": 9})
	if _, err := engine.Run("widget.illegal_column", input); err == nil {
		t.Fatal("expected writing an undeclared column to fail")
	}
}

type illegalColumnWriter struct{}

func (illegalColumnWriter) Info() Info {
	return Info{FunctionID: "widget.illegal_column", Label: "Illegal Column", Columns: []string{"name"}}
}

func (illegalColumnWriter) Run(db store.Session, input json.RawMessage) (any, error) {
	var payload struct {
		ID     int64 `json:"id"`
		Status int   `json:"status"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, err
	}
	return nil, db.Update("widgets", payload.ID, map[string]any{"status": payload.Status})
}

func TestUndoListShrinksToTheLimitAndDropsTheOldest(t *testing.T) {
	engine, _ := newTestEngine(t)
	registerAll(t, engine)
	engine.SetUndoLimit(func() int { return 2 })

	mustRun(t, engine, "widget.create", map[string]any{"name": "one"})
	mustRun(t, engine, "widget.rename", map[string]any{"id": 1, "name": "a"})
	// Undo the rename so the create becomes the oldest step again.
	if _, err := engine.Undo(); err != nil {
		t.Fatalf("undoing: %v", err)
	}
	mustRun(t, engine, "widget.rename", map[string]any{"id": 1, "name": "b"})
	mustRun(t, engine, "widget.rename", map[string]any{"id": 1, "name": "c"})

	if got := engine.Status().UndoSteps; got != 2 {
		t.Fatalf("expected the list trimmed to 2, got %d", got)
	}
}

func TestUndoSkipsStepsWhoseRowsAreGoneForGood(t *testing.T) {
	engine, db := newTestEngine(t)
	registerAll(t, engine)

	// Seed two widgets directly, so only the renames become steps.
	if _, err := db.Exec("INSERT INTO widgets (name) VALUES ('one'), ('two')"); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	mustRun(t, engine, "widget.rename", map[string]any{"id": 1, "name": "a"})
	mustRun(t, engine, "widget.rename", map[string]any{"id": 2, "name": "b"})

	// The newest step's row is destroyed for good.
	if _, err := db.Exec("DELETE FROM widgets WHERE id = 2"); err != nil {
		t.Fatalf("destroying the row: %v", err)
	}

	result, err := engine.Undo()
	if err != nil {
		t.Fatalf("undoing: %v", err)
	}
	if len(result.Skipped) != 1 {
		t.Errorf("expected one skipped note, got %v", result.Skipped)
	}
	if result.Command != "widget.rename" || widgetName(t, db, 1) != "one" {
		t.Errorf("expected the impossible step skipped and the older one applied")
	}
	if engine.Status().UndoSteps != 0 {
		t.Errorf("expected an empty undo list after the skip")
	}
}

func TestForgetRowsDropsEveryStepThatInvolvedThem(t *testing.T) {
	engine, _ := newTestEngine(t)
	registerAll(t, engine)
	mustRun(t, engine, "widget.create", map[string]any{"name": "one"})
	mustRun(t, engine, "widget.rename", map[string]any{"id": 1, "name": "a"})
	if _, err := engine.Undo(); err != nil {
		t.Fatalf("undoing: %v", err)
	}

	engine.ForgetRows("widgets", 1)
	status := engine.Status()
	if status.UndoSteps != 0 || status.RedoSteps != 0 {
		t.Fatalf("expected empty lists, got undo=%d redo=%d", status.UndoSteps, status.RedoSteps)
	}
	if _, err := engine.Undo(); !errors.Is(err, ErrNothingToUndo) {
		t.Errorf("expected ErrNothingToUndo, got %v", err)
	}
}

func TestForgetterIsWiredAfterCommit(t *testing.T) {
	engine, _ := newTestEngine(t)
	registerAll(t, engine)
	mustRun(t, engine, "widget.create", map[string]any{"name": "one"})
	mustRun(t, engine, "widget.rename", map[string]any{"id": 1, "name": "a"})

	if err := engine.Register(destroyForever{}); err != nil {
		t.Fatalf("registering: %v", err)
	}
	if err := engine.Start(); err != nil {
		t.Fatalf("restarting: %v", err)
	}

	mustRun(t, engine, "widget.destroy_forever", map[string]any{"id": 1})
	if engine.Status().UndoSteps != 0 {
		t.Fatalf("expected the destroy to forget the rename step, got %d undo steps",
			engine.Status().UndoSteps)
	}
}

type destroyForever struct{}

func (destroyForever) Info() Info {
	return Info{FunctionID: "widget.destroy_forever", Label: "Destroy Forever"}
}

func (destroyForever) Run(db store.Session, input json.RawMessage) (any, error) {
	var payload struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, err
	}
	if err := db.Delete("widgets", payload.ID); err != nil {
		return nil, err
	}
	return map[string]any{"ids": []int64{payload.ID}}, nil
}

func (destroyForever) ForgetFrom(result any) map[string][]int64 {
	if outcome, ok := result.(map[string]any); ok {
		if ids, ok := outcome["ids"].([]int64); ok {
			return map[string][]int64{"widgets": ids}
		}
	}
	return nil
}

func TestUnknownCommandFails(t *testing.T) {
	engine, _ := newTestEngine(t)
	registerAll(t, engine)
	if _, err := engine.Run("nope", nil); err == nil {
		t.Fatal("expected an unknown-command error")
	}
}

func TestUndoAndRedoOnEmptyLists(t *testing.T) {
	engine, _ := newTestEngine(t)
	registerAll(t, engine)
	if _, err := engine.Undo(); !errors.Is(err, ErrNothingToUndo) {
		t.Fatalf("expected ErrNothingToUndo, got %v", err)
	}
	if _, err := engine.Redo(); !errors.Is(err, ErrNothingToRedo) {
		t.Fatalf("expected ErrNothingToRedo, got %v", err)
	}
}

func TestBootRefusesUnresolvedUndoOperation(t *testing.T) {
	engine, _ := newTestEngine(t)
	if err := engine.Register(lonelyCommand{}); err != nil {
		t.Fatalf("registering: %v", err)
	}
	if err := engine.Start(); err == nil {
		t.Fatal("expected boot to refuse a command whose undo operation does not exist")
	}
}

type lonelyCommand struct{}

func (lonelyCommand) Info() Info {
	return Info{FunctionID: "widget.lonely", Label: "Lonely", UndoOperationID: "widget.nowhere"}
}

func (lonelyCommand) Run(_ store.Session, _ json.RawMessage) (any, error) { return nil, nil }

// ---------------------------------------------------------------------------
// Manifest validation
// ---------------------------------------------------------------------------

func schemaDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`
		CREATE TABLE parent (id INTEGER PRIMARY KEY AUTOINCREMENT);
		CREATE TABLE child (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			parent_id INTEGER NOT NULL,
			FOREIGN KEY (parent_id) REFERENCES parent(id) ON DELETE CASCADE
		);
	`); err != nil {
		t.Fatalf("creating schema: %v", err)
	}
	return db
}

func TestManifestAcceptsTheRealSchema(t *testing.T) {
	db := schemaDB(t)
	manifest := &Manifest{Tables: map[string]TableRule{
		"parent": {Entity: "parent", Children: map[string]ChildRule{"child": {}}},
		"child":  {Entity: "child"},
	}}
	if err := manifest.Validate(db); err != nil {
		t.Fatalf("expected the matching manifest to validate, got %v", err)
	}
}

func TestManifestRefusesAnEdgeTheSchemaDoesNotHave(t *testing.T) {
	db := schemaDB(t)
	manifest := &Manifest{Tables: map[string]TableRule{
		"parent": {Entity: "parent", Children: map[string]ChildRule{"ghost": {}}},
	}}
	if err := manifest.Validate(db); err == nil {
		t.Fatal("expected a declared-but-unreal edge to fail validation")
	}
}

func TestManifestRefusesAnEdgeTheSchemaHasButTheManifestHides(t *testing.T) {
	db := schemaDB(t)
	manifest := &Manifest{Tables: map[string]TableRule{
		"parent": {Entity: "parent"},
		"child":  {Entity: "child"},
	}}
	err := manifest.Validate(db)
	if err == nil {
		t.Fatal("expected an undeclared real cascade edge to fail validation")
	}
}

// ---------------------------------------------------------------------------
// Capturer + Undoer: the reorder shape
// ---------------------------------------------------------------------------

type reorderWidgets struct{}

func (reorderWidgets) Info() Info {
	return Info{
		FunctionID:      "widget.update_order",
		Label:           "Reorder Widgets",
		UndoOperationID: "widget.update_order",
		Columns:         []string{"sort_order"},
	}
}

func (reorderWidgets) CaptureRows(input json.RawMessage) map[string][]int64 {
	var payload struct {
		OrderedIDs []int64 `json:"ordered_ids"`
	}
	if json.Unmarshal(input, &payload) != nil {
		return nil
	}
	return map[string][]int64{"widgets": payload.OrderedIDs}
}

func (reorderWidgets) Run(db store.Session, input json.RawMessage) (any, error) {
	var payload struct {
		OrderedIDs []int64 `json:"ordered_ids"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, err
	}
	query := `UPDATE widgets SET sort_order = CASE id`
	args := make([]any, 0, len(payload.OrderedIDs)*2)
	for index, id := range payload.OrderedIDs {
		query += " WHEN ? THEN ?"
		args = append(args, id, index)
	}
	query += " ELSE sort_order END"
	_, err := db.Exec(query, args...)
	return nil, err
}

// Undo restores the previous order with one symmetric bulk statement.
func (reorderWidgets) Undo(db store.Session, s *Step) error {
	query := `UPDATE widgets SET sort_order = CASE id`
	args := make([]any, 0, len(s.Updated)*2)
	for _, update := range s.Updated {
		order, _ := update.Columns["sort_order"].(int64)
		query += " WHEN ? THEN ?"
		args = append(args, update.ID, order)
	}
	query += " ELSE sort_order END"
	_, err := db.Exec(query, args...)
	return err
}

func TestCapturerAndUndoerRoundTrip(t *testing.T) {
	engine, db := newTestEngine(t)
	registerAll(t, engine)
	if err := engine.Register(reorderWidgets{}); err != nil {
		t.Fatalf("registering: %v", err)
	}
	if err := engine.Start(); err != nil {
		t.Fatalf("restarting: %v", err)
	}

	// Three widgets with distinct existing orders, seeded directly.
	if _, err := db.Exec(`
		INSERT INTO widgets (name, sort_order) VALUES ('a', 10), ('b', 20), ('c', 30)
	`); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	mustRun(t, engine, "widget.update_order", map[string]any{"ordered_ids": []int64{3, 1, 2}})

	orders := func() []int {
		t.Helper()
		rows, err := db.Query("SELECT sort_order FROM widgets WHERE id IN (1,2,3) ORDER BY id")
		if err != nil {
			t.Fatalf("reading orders: %v", err)
		}
		defer rows.Close()
		var orders []int
		for rows.Next() {
			var order int
			_ = rows.Scan(&order)
			orders = append(orders, order)
		}
		return orders
	}

	if got := orders(); fmt.Sprint(got) != "[1 2 0]" {
		t.Fatalf("expected the new order applied, got %v", got)
	}

	if _, err := engine.Undo(); err != nil {
		t.Fatalf("undoing the reorder: %v", err)
	}
	if got := orders(); fmt.Sprint(got) != "[10 20 30]" {
		t.Fatalf("expected the previous order restored by the bulk undo, got %v", got)
	}
}
