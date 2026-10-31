package commands

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/wailsapp/wails/v3/pkg/application"
	"gopkg.in/yaml.v3"
	"snap-rq/backend/store"
)

// This file is the engine: it runs commands and remembers how to undo
// them. Read it top to bottom.
//
// Run:   lock writers → open a transaction → the command writes through a
//        recording session, which saves created rows, deleted rows and old
//        column values into a Step → commit → record the Step → notify the
//        UI. Read-only commands skip all of it: no lock, no transaction,
//        no Step — so a slow request send never blocks a rename.
//
// Undo:  reverse the newest Step — delete the rows it created, write back
//        the old column values, re-insert the rows it deleted — in one
//        transaction. A command may reverse itself (the Undoer interface)
//        when one bulk statement beats row-by-row restoration.
//
// Redo:  repeat the command from its recorded input; the rows exist again
//        under the same ids. Pure creates re-insert their saved rows so
//        the ids never change.
//
// Sections: 1. the Engine  2. the recorded Step  3. the public API
// 4. running a command  5. undo and redo  6. the recording session
// 7. row helpers.

//go:embed manifest.yaml
var manifestYAML []byte

// ErrNothingToUndo is returned by Undo on an empty list.
var ErrNothingToUndo = errors.New("nothing to undo")

// ErrNothingToRedo is returned by Redo on an empty list.
var ErrNothingToRedo = errors.New("nothing to redo")

// ---------------------------------------------------------------------------
// 1. The Engine
// ---------------------------------------------------------------------------

// Engine runs every command by name and owns the undo store. Writers are
// serialized (SQLite allows one writer at a time anyway); read-only
// commands run without the lock.
type Engine struct {
	db       *sql.DB
	manifest *Manifest
	store    *UndoStore

	mu      sync.Mutex
	known   map[string]Command
	started bool

	lastAction string // "run", "undo" or "redo" — drives the undo toast
	lastLabel  string
}

// NewEngine returns an engine for the given database, with the cascade
// manifest parsed from the embedded YAML. Call Register for every command,
// then Start, which validates everything and refuses to run the app on a
// mismatch.
func NewEngine(db *sql.DB) (*Engine, error) {
	var manifest Manifest
	if err := yaml.Unmarshal(manifestYAML, &manifest); err != nil {
		return nil, fmt.Errorf("parsing the command manifest: %w", err)
	}
	return &Engine{
		db:       db,
		manifest: &manifest,
		store:    &UndoStore{},
		known:    map[string]Command{},
	}, nil
}

// NewEngineWithManifest is NewEngine with an explicit manifest, for tests.
func NewEngineWithManifest(db *sql.DB, manifest *Manifest) *Engine {
	return &Engine{db: db, manifest: manifest, store: &UndoStore{}, known: map[string]Command{}}
}

// Register makes commands runnable by name. Duplicate ids are an error.
func (e *Engine) Register(cmds ...Command) error {
	for _, cmd := range cmds {
		info := cmd.Info()
		if info.FunctionID == "" {
			return errors.New("a command has no FunctionID")
		}
		if _, exists := e.known[info.FunctionID]; exists {
			return fmt.Errorf("command %s is already registered", info.FunctionID)
		}
		e.known[info.FunctionID] = cmd
	}
	return nil
}

// SetUndoLimit installs the settings-backed undo list size.
func (e *Engine) SetUndoLimit(fn func() int) {
	e.store.SetUndoLimit(fn)
}

// Start validates the whole framework and must be called after every
// Register: the manifest must match the real schema, and every
// UndoOperationID must resolve. A failure means the app must not run.
func (e *Engine) Start() error {
	if err := e.manifest.Validate(e.db); err != nil {
		return fmt.Errorf("command manifest: %w", err)
	}
	for _, cmd := range e.known {
		info := cmd.Info()
		if info.UndoOperationID == "" {
			continue
		}
		if _, ok := e.known[info.UndoOperationID]; !ok {
			return fmt.Errorf("command %s names %q as its undo operation, but no such command is registered",
				info.FunctionID, info.UndoOperationID)
		}
	}
	e.started = true
	return nil
}

// DB exposes the database for callers that need direct access (wiring
// remaining services during the migration).
func (e *Engine) DB() *sql.DB {
	return e.db
}

// CommandInfo is one entry of the catalog, for callers that ask "what can
// this app do?".
type CommandInfo struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Undoable bool   `json:"undoable"`
	ReadOnly bool   `json:"readOnly"`
}

// ListCommands returns the catalog.
func (e *Engine) ListCommands() []CommandInfo {
	infos := make([]CommandInfo, 0, len(e.known))
	for _, cmd := range e.known {
		info := cmd.Info()
		infos = append(infos, CommandInfo{
			Name:     info.FunctionID,
			Label:    info.Label,
			Undoable: info.UndoOperationID != "",
			ReadOnly: info.ReadOnly,
		})
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	return infos
}

// ---------------------------------------------------------------------------
// 2. The recorded Step — everything undo needs
// ---------------------------------------------------------------------------

// SavedRow is a saved copy of one database row, as it was stored.
type SavedRow struct {
	Table  string
	Values map[string]any // column → value
}

// ColumnChange remembers the previous values of one row's changed columns.
type ColumnChange struct {
	Table     string
	ID        int64
	OldValues map[string]any
}

// Step is one recorded command run: what the command was invoked with and
// the three facts about what it did. Undo reverses exactly these facts.
type Step struct {
	FunctionID string
	Label      string
	When       time.Time
	Input      json.RawMessage // the payload the command was invoked with

	RowsCreated   []SavedRow     // rows it inserted, in order
	RowsDeleted   []SavedRow     // rows it deleted, children first
	ColumnChanges []ColumnChange // old values of the columns it changed

	// rowsKey names the rows involved; quick repeats of the same command
	// on the same rows share it and merge into one step.
	rowsKey string
}

// ---------------------------------------------------------------------------
// 3. The public API
// ---------------------------------------------------------------------------

// Run executes a command by name.
func (e *Engine) Run(name string, input json.RawMessage) (any, error) {
	cmd, ok := e.known[name]
	if !ok {
		return nil, fmt.Errorf("unknown command %q", name)
	}
	info := cmd.Info()

	if info.ReadOnly {
		// No lock, no transaction, no recording — reads and external-only
		// calls (a request send) never block a writer. Writing through
		// this session is a programming error caught immediately.
		session := &recordingSession{Session: store.Wrap(e.db), engine: e, readOnly: true}
		return cmd.Run(session, input)
	}

	result, err := e.runLocked(func() (any, []event, error) {
		return e.runCommand(cmd, info, input)
	})
	return result, err
}

// Undo reverses the newest step and reports what happened.
func (e *Engine) Undo() (UndoResult, error) {
	result, err := e.runLocked(func() (any, []event, error) {
		return e.undoNewestStep()
	})
	if err != nil {
		return UndoResult{}, err
	}
	return result.(UndoResult), nil
}

// Redo repeats the newest undone step.
func (e *Engine) Redo() (UndoResult, error) {
	result, err := e.runLocked(func() (any, []event, error) {
		return e.redoNewestStep()
	})
	if err != nil {
		return UndoResult{}, err
	}
	return result.(UndoResult), nil
}

// Status returns the current undo-menu summary.
func (e *Engine) Status() Status {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.store.Status(e.lastAction, e.lastLabel)
}

// Steps lists every remembered step, oldest first, for the panel.
func (e *Engine) Steps() []StepInfo {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.store.Steps()
}

// ForgetRows drops every undo step that involves the given rows — used
// when the user destroys data for good, so undo can never bring it back.
func (e *Engine) ForgetRows(table string, ids ...int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.store.ForgetRows(table, ids...)
}

// runLocked serializes writers and sends events only after unlocking: a
// busy window must never stall the next command. The lock pattern lives
// here, once, instead of in every entry point.
func (e *Engine) runLocked(work func() (any, []event, error)) (any, error) {
	e.mu.Lock()
	result, events, err := work()
	e.mu.Unlock()
	emit(events)
	return result, err
}

// ---------------------------------------------------------------------------
// 4. Running a command
// ---------------------------------------------------------------------------

// runCommand is the whole write path, in order: transaction → capture for
// bulk commands → the command itself → commit → forget destroyed rows →
// record the step → events.
func (e *Engine) runCommand(cmd Command, info Info, input json.RawMessage) (any, []event, error) {
	tx, err := e.db.Begin()
	if err != nil {
		return nil, nil, fmt.Errorf("starting the transaction: %w", err)
	}
	defer tx.Rollback()

	step := &Step{FunctionID: info.FunctionID, Label: info.Label, When: time.Now(), Input: input}
	session := &recordingSession{
		Session: store.Wrap(tx),
		engine:  e,
		step:    step,
		columns: info.Columns,
	}

	// Commands whose writes are one bulk statement save the current values
	// of the rows they touch before running.
	if capturer, ok := cmd.(Capturer); ok {
		if err := e.saveCurrentValues(tx, step, info, capturer.CaptureRows(input)); err != nil {
			return nil, nil, err
		}
	}

	result, err := cmd.Run(session, input)
	if err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("committing: %w", err)
	}

	// Rows destroyed for good drop the steps that involve them.
	if forgetter, ok := cmd.(Forgetter); ok {
		for table, ids := range forgetter.ForgetFrom(result) {
			e.store.ForgetRows(table, ids...)
		}
	}

	undoable := info.UndoOperationID != ""
	if undoable {
		step.rowsKey = step.computeRowsKey()
		e.store.Record(step)
		e.lastAction, e.lastLabel = "run", info.Label
	}
	return result, e.eventsFor(step, undoable), nil
}

// eventsFor assembles what the UI hears about a run: which entities
// changed, and the new undo-menu summary.
func (e *Engine) eventsFor(step *Step, undoable bool) []event {
	if len(step.RowsCreated) > 0 || len(step.RowsDeleted) > 0 || len(step.ColumnChanges) > 0 {
		return []event{
			{EventChanged, CommandChanged{Command: step.FunctionID, Changes: e.uiChanges(step)}},
			{EventHistory, e.store.Status(e.lastAction, e.lastLabel)},
		}
	}
	if undoable {
		return []event{{EventHistory, e.store.Status(e.lastAction, e.lastLabel)}}
	}
	return nil
}

// ---------------------------------------------------------------------------
// 5. Undo and redo
// ---------------------------------------------------------------------------

// undoNewestStep reverses the newest step. Steps whose changed rows are
// gone for good are skipped with a note; a failed reversal changes nothing
// (it is one transaction) and the step stays.
func (e *Engine) undoNewestStep() (UndoResult, []event, error) {
	var skipped []string
	for {
		step := e.store.PopUndo()
		if step == nil {
			return UndoResult{Skipped: skipped}, nil, ErrNothingToUndo
		}
		cmd := e.known[step.FunctionID]

		// Rows a step changed must still exist; rows it deleted or created
		// are handled by the reversal itself (loud failure or re-delete).
		if err := e.changedRowsStillExist(step); err != nil {
			skipped = append(skipped, fmt.Sprintf("Skipped %q: %v", step.Label, err))
			continue
		}

		err := e.inTransaction(func(tx *sql.Tx) error {
			session := store.Wrap(tx)
			if undoer, ok := cmd.(Undoer); ok {
				return undoer.Undo(session, step)
			}
			return e.reverseStep(session, step)
		})
		if err != nil {
			// Nothing happened; the step goes back and the error is shown.
			e.store.PushBackUndo(step)
			return UndoResult{Skipped: skipped}, nil, fmt.Errorf("cannot undo %s: %w", step.FunctionID, err)
		}

		e.store.PushRedo(step)
		e.lastAction, e.lastLabel = "undo", step.Label
		events := []event{
			{EventChanged, CommandChanged{Command: step.FunctionID, Changes: e.uiChanges(step)}},
			{EventHistory, e.store.Status(e.lastAction, e.lastLabel)},
		}
		return UndoResult{Command: step.FunctionID, Label: step.Label, Skipped: skipped}, events, nil
	}
}

// redoNewestStep repeats the newest undone step.
func (e *Engine) redoNewestStep() (UndoResult, []event, error) {
	step := e.store.PopRedo()
	if step == nil {
		return UndoResult{}, nil, ErrNothingToRedo
	}
	cmd := e.known[step.FunctionID]

	err := e.inTransaction(func(tx *sql.Tx) error {
		session := store.Wrap(tx)
		if redoer, ok := cmd.(Redoer); ok {
			return redoer.Redo(session, step)
		}
		return e.repeatStep(session, cmd, step)
	})
	if err != nil {
		e.store.PushBackRedo(step)
		return UndoResult{}, nil, fmt.Errorf("cannot redo %s: %w", step.FunctionID, err)
	}

	e.store.PushUndo(step)
	e.lastAction, e.lastLabel = "redo", step.Label
	events := []event{
		{EventChanged, CommandChanged{Command: step.FunctionID, Changes: e.uiChanges(step)}},
		{EventHistory, e.store.Status(e.lastAction, e.lastLabel)},
	}
	return UndoResult{Command: step.FunctionID, Label: step.Label}, events, nil
}

// changedRowsStillExist checks that every row a step updated is still
// there. A missing row means the data was destroyed for good and the step
// cannot be undone.
func (e *Engine) changedRowsStillExist(step *Step) error {
	for _, change := range step.ColumnChanges {
		var one int
		err := e.db.QueryRow(
			fmt.Sprintf("SELECT 1 FROM %s WHERE id = ?", change.Table), change.ID,
		).Scan(&one)
		if err == sql.ErrNoRows {
			return fmt.Errorf("%s #%d no longer exists", change.Table, change.ID)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// reverseStep undoes a step from its three recorded facts, in one
// transaction: delete the created rows (last one first), write the old
// values back onto changed rows, re-insert the deleted rows (parents
// first — the reverse of the saved order). A plain INSERT, not INSERT OR
// REPLACE: if a row is somehow back already, failing loudly beats
// silently destroying the rows that point at it.
func (e *Engine) reverseStep(db store.Session, step *Step) error {
	for i := len(step.RowsCreated) - 1; i >= 0; i-- {
		row := step.RowsCreated[i]
		query, args, err := squirrel.Delete(row.Table).Where(squirrel.Eq{"id": rowID(row.Values)}).ToSql()
		if err != nil {
			return err
		}
		if _, err := db.Exec(query, args...); err != nil {
			return fmt.Errorf("deleting %s #%d again: %w", row.Table, rowID(row.Values), err)
		}
	}

	for _, change := range step.ColumnChanges {
		if err := db.Update(change.Table, change.ID, change.OldValues); err != nil {
			return err
		}
	}

	for i := len(step.RowsDeleted) - 1; i >= 0; i-- {
		row := step.RowsDeleted[i]
		if _, err := db.Insert(row.Table, row.Values); err != nil {
			return err
		}
	}
	return nil
}

// repeatStep redoes a step: commands that deleted or changed rows re-run
// from their recorded input (the rows exist again under the same ids);
// pure creates re-insert their saved rows so the ids stay the same.
func (e *Engine) repeatStep(db store.Session, cmd Command, step *Step) error {
	if len(step.RowsDeleted) > 0 || len(step.ColumnChanges) > 0 {
		_, err := cmd.Run(db, step.Input)
		return err
	}
	for _, row := range step.RowsCreated {
		if _, err := db.Insert(row.Table, row.Values); err != nil {
			return err
		}
	}
	return nil
}

// inTransaction runs one unit of work as all-or-nothing: any error rolls
// everything back.
func (e *Engine) inTransaction(work func(tx *sql.Tx) error) error {
	tx, err := e.db.Begin()
	if err != nil {
		return fmt.Errorf("starting the transaction: %w", err)
	}
	defer tx.Rollback()
	if err := work(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// ---------------------------------------------------------------------------
// 6. The recording session — how the engine learns what a command did
// ---------------------------------------------------------------------------

// recordingSession is the database session a command runs with. Every
// write goes through the database AND is recorded in the step, so undo can
// reverse it later. Read-only commands get the same session with writes
// refused.
type recordingSession struct {
	store.Session
	engine   *Engine
	step     *Step
	columns  []string // the command's declared writable columns
	readOnly bool
}

// Insert stores the row and records it as created (the full stored row is
// read back, so database defaults are included).
func (s *recordingSession) Insert(table string, values map[string]any) (int64, error) {
	if s.readOnly {
		return 0, fmt.Errorf("a read-only command tried to insert into %s", table)
	}
	id, err := s.Session.Insert(table, values)
	if err != nil {
		return 0, err
	}
	row, err := readRow(s.Session, table, id)
	if err != nil {
		return 0, err
	}
	s.step.RowsCreated = append(s.step.RowsCreated, SavedRow{Table: table, Values: row})
	return id, nil
}

// Delete removes the rows, after saving copies of them and of everything
// the cascades will remove with them.
func (s *recordingSession) Delete(table string, ids ...int64) error {
	if s.readOnly {
		return fmt.Errorf("a read-only command tried to delete from %s", table)
	}
	if len(ids) == 0 {
		return nil
	}
	if err := s.engine.saveRowsBeforeDelete(s.Session, s.step, table, ids); err != nil {
		return err
	}
	return s.Session.Delete(table, ids...)
}

// Update writes the declared columns onto the row, after saving the row's
// previous values of exactly those columns.
func (s *recordingSession) Update(table string, id int64, columns map[string]any) error {
	if s.readOnly {
		return fmt.Errorf("a read-only command tried to update %s", table)
	}
	if err := s.ensureOwnedColumns(table, columns); err != nil {
		return err
	}
	row, err := readRow(s.Session, table, id)
	if err != nil {
		return err
	}
	old := map[string]any{}
	for column := range columns {
		old[column] = row[column]
	}
	s.step.ColumnChanges = append(s.step.ColumnChanges, ColumnChange{Table: table, ID: id, OldValues: old})
	return s.Session.Update(table, id, columns)
}

// ensureOwnedColumns enforces column ownership: a command may only write
// the columns it declared. That is what keeps a send's status_code safe
// from the undo of a user edit — and the other way around.
func (s *recordingSession) ensureOwnedColumns(table string, columns map[string]any) error {
	for name := range columns {
		allowed := false
		for _, declared := range s.columns {
			if name == declared {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("command %s may not write %s.%s (declared columns: %s)",
				s.step.FunctionID, table, name, strings.Join(s.columns, ", "))
		}
	}
	return nil
}

// saveRowsBeforeDelete saves copies of the given rows and of every row the
// cascades will remove with them — children first, so undo re-inserts
// parents before the children that point at them. The edges come from the
// manifest; their key columns were resolved from the real schema at boot.
func (e *Engine) saveRowsBeforeDelete(db store.Session, step *Step, table string, ids []int64) error {
	for _, edge := range e.manifest.edgesFor(table) {
		if !edge.Restore {
			continue // a real cascade, but undo never restores it
		}
		childIDs, err := readChildIDs(db, edge.Child, edge.FKColumn, ids)
		if err != nil {
			return err
		}
		if err := e.saveRowsBeforeDelete(db, step, edge.Child, childIDs); err != nil {
			return err
		}
	}

	rows, err := readRows(db, table, ids)
	if err != nil {
		return err
	}
	for _, values := range rows {
		step.RowsDeleted = append(step.RowsDeleted, SavedRow{Table: table, Values: values})
	}
	return nil
}

// saveCurrentValues saves the current values of the given rows' declared
// columns — the Capturer support for commands whose writes are one bulk
// statement.
func (e *Engine) saveCurrentValues(tx *sql.Tx, step *Step, info Info, tables map[string][]int64) error {
	db := store.Wrap(tx)
	for _, table := range sortedNames(tables) {
		ids := tables[table]
		if len(ids) == 0 {
			continue
		}
		rows, err := readRows(db, table, ids)
		if err != nil {
			return err
		}
		for _, values := range rows {
			old := map[string]any{}
			for _, column := range info.Columns {
				old[column] = values[column]
			}
			id, _ := values["id"].(int64)
			step.ColumnChanges = append(step.ColumnChanges, ColumnChange{Table: table, ID: id, OldValues: old})
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// 7. Row helpers
// ---------------------------------------------------------------------------

// readRows reads full copies of the given rows, ordered by id. This is the
// only place in the engine that turns query results into maps.
func readRows(db store.Session, table string, ids []int64) ([]map[string]any, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	query, args, err := squirrel.Select("*").From(table).Where(squirrel.Eq{"id": ids}).OrderBy("id").ToSql()
	if err != nil {
		return nil, fmt.Errorf("building the query: %w", err)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("reading %s rows: %w", table, err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	idIndex := -1
	for i, column := range columns {
		if column == "id" {
			idIndex = i
			break
		}
	}
	if idIndex == -1 {
		return nil, fmt.Errorf("table %s has no id column", table)
	}

	var result []map[string]any
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(values))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, fmt.Errorf("reading a %s row: %w", table, err)
		}
		if _, ok := values[idIndex].(int64); !ok {
			return nil, fmt.Errorf("table %s has a non-integer id column", table)
		}
		copied := make(map[string]any, len(columns))
		for i, column := range columns {
			copied[column] = values[i]
		}
		result = append(result, copied)
	}
	return result, rows.Err()
}

// readRow reads one full row.
func readRow(db store.Session, table string, id int64) (map[string]any, error) {
	rows, err := readRows(db, table, []int64{id})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%s #%d no longer exists", table, id)
	}
	return rows[0], nil
}

// readChildIDs lists the ids of one table's rows that point at the given
// parent rows.
func readChildIDs(db store.Session, child, foreignKey string, parentIDs []int64) ([]int64, error) {
	if len(parentIDs) == 0 {
		return nil, nil
	}
	query, args, err := squirrel.Select("id").From(child).Where(squirrel.Eq{foreignKey: parentIDs}).ToSql()
	if err != nil {
		return nil, fmt.Errorf("building the query: %w", err)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing %s rows: %w", child, err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("reading a %s id: %w", child, err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// rowID returns a saved row's id.
func rowID(values map[string]any) int64 {
	id, _ := values["id"].(int64)
	return id
}

func sortedNames(m map[string][]int64) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ---------------------------------------------------------------------------
// Events — how the UI hears about runs, undos and redos
// ---------------------------------------------------------------------------

const (
	// EventChanged fires after every command that changed something,
	// telling the UI which entities to refresh (ids only, never values).
	EventChanged = "command:changed"
	// EventHistory fires whenever the undo list changes.
	EventHistory = "command:history"
)

// EntityChange notes what the user will see change, so the UI can refresh
// it. Only ids, never values — variable values can be secrets.
type EntityChange struct {
	Entity string  `json:"entity"` // e.g. "request"
	Op     string  `json:"op"`     // "create", "update" or "delete"
	IDs    []int64 `json:"ids"`
}

// CommandChanged is the payload of EventChanged.
type CommandChanged struct {
	Command string         `json:"command"`
	Changes []EntityChange `json:"changes"`
}

// event is one Wails event waiting to be sent after the lock is released.
// Sending can wait on a busy window, and no command should ever queue
// behind that.
type event struct {
	name string
	data any
}

func emit(events []event) {
	app := application.Get()
	if app == nil {
		return // the app is not running (tests)
	}
	for _, e := range events {
		app.Event.Emit(e.name, e.data)
	}
}

// uiChanges derives what the user will see change from a step's three
// recorded facts, grouped per entity and operation.
func (e *Engine) uiChanges(step *Step) []EntityChange {
	byEntityOp := map[string]map[string][]int64{}
	add := func(table, op string, ids ...int64) {
		entity := e.manifest.entity(table)
		if byEntityOp[entity] == nil {
			byEntityOp[entity] = map[string][]int64{}
		}
		byEntityOp[entity][op] = append(byEntityOp[entity][op], ids...)
	}
	for _, row := range step.RowsCreated {
		add(row.Table, "create", rowID(row.Values))
	}
	for _, row := range step.RowsDeleted {
		add(row.Table, "delete", rowID(row.Values))
	}
	for _, change := range step.ColumnChanges {
		add(change.Table, "update", change.ID)
	}

	changes := make([]EntityChange, 0, len(byEntityOp))
	for entity, ops := range byEntityOp {
		for op, ids := range ops {
			if len(ids) > 0 {
				changes = append(changes, EntityChange{Entity: entity, Op: op, IDs: ids})
			}
		}
	}
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Entity != changes[j].Entity {
			return changes[i].Entity < changes[j].Entity
		}
		return changes[i].Op < changes[j].Op
	})
	return changes
}
