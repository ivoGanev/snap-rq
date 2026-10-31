package commands

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	_ "embed"

	"github.com/wailsapp/wails/v3/pkg/application"
	"gopkg.in/yaml.v3"
	"snap-rq/backend/store"
)

//go:embed manifest.yaml
var manifestYAML []byte

// ErrNothingToUndo is returned by Undo on an empty list.
var ErrNothingToUndo = errors.New("nothing to undo")

// ErrNothingToRedo is returned by Redo on an empty list.
var ErrNothingToRedo = errors.New("nothing to redo")

const (
	// EventChanged fires after every command that changed something,
	// telling the UI which entities to refresh (ids only, never values).
	EventChanged = "command:changed"
	// EventHistory fires whenever the undo list changes.
	EventHistory = "command:history"
)

// Engine runs commands and owns the undo store. Writers are serialized
// (SQLite is single-writer anyway); ReadOnly commands run lock-free so
// queries — and slow external calls like request sends — never queue
// behind a write.
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

// DB exposes the database for callers that need direct access (wiring
// remaining services during the migration).
func (e *Engine) DB() *sql.DB {
	return e.db
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

// Touched notes what the user will see change, so the UI can refresh it.
// Only ids, never values — variable values can be secrets.
type Touched struct {
	Entity string  `json:"entity"` // e.g. "request"
	Op     string  `json:"op"`     // "create", "update" or "delete"
	IDs    []int64 `json:"ids"`
}

// CommandChanged is the payload of EventChanged.
type CommandChanged struct {
	Command string    `json:"command"`
	Changes []Touched `json:"changes"`
}

// Run executes a command by name.
func (e *Engine) Run(name string, input json.RawMessage) (any, error) {
	cmd, ok := e.known[name]
	if !ok {
		return nil, fmt.Errorf("unknown command %q", name)
	}
	info := cmd.Info()

	if info.ReadOnly {
		// No lock, no transaction, no recording: reads and external-only
		// calls (a request send) never block a writer.
		session := store.Wrap(e.db)
		return cmd.Run(&readonlySession{Session: session}, input)
	}

	e.mu.Lock()
	result, events, err := e.runWrite(cmd, info, input)
	e.mu.Unlock()
	emit(events)
	return result, err
}

func (e *Engine) runWrite(cmd Command, info Info, input json.RawMessage) (any, []event, error) {
	tx, err := e.db.Begin()
	if err != nil {
		return nil, nil, fmt.Errorf("starting the transaction: %w", err)
	}
	defer tx.Rollback()

	step := &Step{FunctionID: info.FunctionID, Label: info.Label, When: time.Now(), Input: input}
	session := &observedSession{
		Session: store.Wrap(tx),
		engine:  e,
		step:    step,
		columns: info.Columns,
	}

	// Commands with bulk raw SQL photograph their rows first.
	var events []event
	if capturer, ok := cmd.(Capturer); ok {
		if err := e.captureDeclared(tx, step, info, capturer.CaptureRows(input)); err != nil {
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

	// Destroyed-for-good rows drop the undo steps that involve them.
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

	if len(step.Created) > 0 || len(step.Deleted) > 0 || len(step.Updated) > 0 {
		events = []event{
			{EventChanged, CommandChanged{Command: info.FunctionID, Changes: e.touched(step)}},
			{EventHistory, e.store.Status(e.lastAction, e.lastLabel)},
		}
	} else if undoable {
		events = []event{{EventHistory, e.store.Status(e.lastAction, e.lastLabel)}}
	}
	return result, events, nil
}

// touched derives what the user will see change from what the step
// recorded: creates, deletes and updates per entity.
func (e *Engine) touched(step *Step) []Touched {
	byEntityOp := map[string]map[string][]int64{}
	add := func(table, op string, ids ...int64) {
		entity := e.manifest.entity(table)
		if byEntityOp[entity] == nil {
			byEntityOp[entity] = map[string][]int64{}
		}
		byEntityOp[entity][op] = append(byEntityOp[entity][op], ids...)
	}
	for _, row := range step.Created {
		add(row.Table, "create", rowID(row.Values))
	}
	for _, set := range step.Deleted {
		ids := make([]int64, 0, len(set.Rows))
		for _, values := range set.Rows {
			ids = append(ids, rowID(values))
		}
		add(set.Table, "delete", ids...)
	}
	for _, update := range step.Updated {
		add(update.Table, "update", update.ID)
	}

	changes := make([]Touched, 0, len(byEntityOp))
	for entity, ops := range byEntityOp {
		for op, ids := range ops {
			if len(ids) > 0 {
				changes = append(changes, Touched{Entity: entity, Op: op, IDs: ids})
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

// Undo reverses the newest step. Steps whose updated rows are gone for
// good are skipped with a note; a failed restore changes nothing (it is
// one transaction) and the step stays.
func (e *Engine) Undo() (UndoResult, error) {
	e.mu.Lock()
	result, events, err := e.undoLocked()
	e.mu.Unlock()
	emit(events)
	return result, err
}

func (e *Engine) undoLocked() (UndoResult, []event, error) {
	var skipped []string
	for {
		step := e.store.PopUndo()
		if step == nil {
			return UndoResult{Skipped: skipped}, nil, ErrNothingToUndo
		}
		cmd := e.known[step.FunctionID]

		// Rows a step updated must still exist; rows it deleted or created
		// are handled by the restore itself (loud failure or re-delete).
		if err := e.updatedRowsExist(step); err != nil {
			// The rows are gone for good; undoing this step is
			// impossible. Forget it and try the next one.
			skipped = append(skipped, fmt.Sprintf("Skipped %q: %v", step.Label, err))
			continue
		}

		err := e.inRestoreTx(func(tx *sql.Tx) error {
			session := store.Wrap(tx)
			if undoer, ok := cmd.(Undoer); ok {
				return undoer.Undo(session, step)
			}
			return e.defaultUndo(session, step)
		})
		if err != nil {
			// Nothing happened; the step goes back and the error is shown.
			e.store.PushBackUndo(step)
			return UndoResult{Skipped: skipped}, nil, fmt.Errorf("cannot undo %s: %w", step.FunctionID, err)
		}

		e.store.PushRedo(step)
		e.lastAction, e.lastLabel = "undo", step.Label
		events := []event{
			{EventChanged, CommandChanged{Command: step.FunctionID, Changes: e.touched(step)}},
			{EventHistory, e.store.Status(e.lastAction, e.lastLabel)},
		}
		return UndoResult{Command: step.FunctionID, Label: step.Label, Skipped: skipped}, events, nil
	}
}

// updatedRowsExist checks that the rows a step updated are still there.
func (e *Engine) updatedRowsExist(step *Step) error {
	for _, update := range step.Updated {
		var one int
		err := e.db.QueryRow(
			fmt.Sprintf("SELECT 1 FROM %s WHERE id = ?", update.Table), update.ID,
		).Scan(&one)
		if err == sql.ErrNoRows {
			return fmt.Errorf("%s #%d no longer exists", update.Table, update.ID)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// Redo re-applies the newest undone step.
func (e *Engine) Redo() (UndoResult, error) {
	e.mu.Lock()
	result, events, err := e.redoLocked()
	e.mu.Unlock()
	emit(events)
	return result, err
}

func (e *Engine) redoLocked() (UndoResult, []event, error) {
	step := e.store.PopRedo()
	if step == nil {
		return UndoResult{}, nil, ErrNothingToRedo
	}
	cmd := e.known[step.FunctionID]

	err := e.inRestoreTx(func(tx *sql.Tx) error {
		session := store.Wrap(tx)
		if redoer, ok := cmd.(Redoer); ok {
			return redoer.Redo(session, step)
		}
		return e.defaultRedo(session, cmd, step)
	})
	if err != nil {
		e.store.PushBackRedo(step)
		return UndoResult{}, nil, fmt.Errorf("cannot redo %s: %w", step.FunctionID, err)
	}

	e.store.PushUndo(step)
	e.lastAction, e.lastLabel = "redo", step.Label
	events := []event{
		{EventChanged, CommandChanged{Command: step.FunctionID, Changes: e.touched(step)}},
		{EventHistory, e.store.Status(e.lastAction, e.lastLabel)},
	}
	return UndoResult{Command: step.FunctionID, Label: step.Label}, events, nil
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

// ForgetRows drops every undo step involving the given rows.
func (e *Engine) ForgetRows(table string, ids ...int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.store.ForgetRows(table, ids...)
}

func (e *Engine) inRestoreTx(apply func(tx *sql.Tx) error) error {
	tx, err := e.db.Begin()
	if err != nil {
		return fmt.Errorf("starting the restore transaction: %w", err)
	}
	defer tx.Rollback()
	if err := apply(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// defaultUndo reverses a step from what it recorded, in one transaction:
//
//   - rows the command created are deleted again, last-created first
//   - rows the command updated get their old column values back
//   - cascade trees the command deleted are inserted back, parents first,
//     with their original ids
//
// A plain INSERT (not INSERT OR REPLACE) on purpose — if a row is somehow
// back already, failing loudly beats silently destroying the rows that
// point at it.
func (e *Engine) defaultUndo(db store.Session, step *Step) error {
	for i := len(step.Created) - 1; i >= 0; i-- {
		row := step.Created[i]
		id := rowID(row.Values)
		if _, err := db.Exec(fmt.Sprintf("DELETE FROM %s WHERE id = ?", row.Table), id); err != nil {
			return fmt.Errorf("deleting %s #%d again: %w", row.Table, id, err)
		}
	}

	for _, update := range step.Updated {
		if err := db.Update(update.Table, update.ID, update.Columns); err != nil {
			return err
		}
	}

	for i := len(step.Deleted) - 1; i >= 0; i-- {
		set := step.Deleted[i]
		for _, values := range set.Rows {
			if err := insertRow(db, set.Table, values); err != nil {
				return err
			}
		}
	}
	return nil
}

// defaultRedo re-applies a step: commands that deleted or updated re-run
// from their recorded input (the rows exist again under the same ids);
// pure creates re-insert their recorded rows verbatim so ids stay stable.
func (e *Engine) defaultRedo(db store.Session, cmd Command, step *Step) error {
	if len(step.Deleted) > 0 || len(step.Updated) > 0 {
		_, err := cmd.Run(db, step.Input)
		return err
	}
	for _, row := range step.Created {
		if err := insertRow(db, row.Table, row.Values); err != nil {
			return err
		}
	}
	return nil
}

func insertRow(db store.Session, table string, values map[string]any) error {
	columns := make([]string, 0, len(values))
	args := make([]any, 0, len(values))
	for column, value := range values {
		columns = append(columns, column)
		args = append(args, value)
	}
	sort.Strings(columns)
	placeholders := make([]string, len(columns))
	for i := range placeholders {
		placeholders[i] = "?"
	}
	sortedArgs := make([]any, len(columns))
	for i, column := range columns {
		sortedArgs[i] = values[column]
	}

	_, err := db.Exec(
		fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
			table, strings.Join(columns, ", "), strings.Join(placeholders, ", ")),
		sortedArgs...,
	)
	if err != nil {
		return fmt.Errorf("inserting %s #%d back: %w", table, rowID(values), err)
	}
	return nil
}

// captureDeclared photographs the rows a Capturer command will touch,
// reading only the command's declared columns — those are the values undo
// writes back.
func (e *Engine) captureDeclared(tx *sql.Tx, step *Step, info Info, tables map[string][]int64) error {
	for _, table := range sortedTableNames(tables) {
		ids := tables[table]
		if len(ids) == 0 {
			continue
		}
		columns := info.Columns
		if len(columns) == 0 {
			columns = []string{"id"}
		}
		query := fmt.Sprintf("SELECT id, %s FROM %s WHERE id IN (%s)",
			strings.Join(columns, ", "), table, placeholders(len(ids)))
		args := make([]any, len(ids))
		for i, id := range ids {
			args[i] = id
		}
		rows, err := tx.Query(query, args...)
		if err != nil {
			return fmt.Errorf("capturing %s: %w", table, err)
		}
		for rows.Next() {
			scan := make([]any, len(columns)+1)
			pointers := make([]any, len(scan))
			for i := range scan {
				pointers[i] = &scan[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				rows.Close()
				return fmt.Errorf("capturing %s: %w", table, err)
			}
			id, _ := scan[0].(int64)
			old := map[string]any{}
			for i, column := range columns {
				old[column] = scan[i+1]
			}
			step.Updated = append(step.Updated, Update{Table: table, ID: id, Columns: old})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("capturing %s: %w", table, err)
		}
		rows.Close()
	}
	return nil
}

func sortedTableNames(tables map[string][]int64) []string {
	names := make([]string, 0, len(tables))
	for name := range tables {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// ---------------------------------------------------------------------------
// The observed session: what the engine learns while a command works.
// ---------------------------------------------------------------------------

// observedSession wraps the plain session for one undoable (or at least
// recording) command run: every Insert, Delete and Update is recorded in
// the step, so undo can reverse it.
type observedSession struct {
	store.Session
	engine  *Engine
	step    *Step
	columns []string // the running command's declared writable columns
}

func (s *observedSession) Insert(table string, values map[string]any) (int64, error) {
	id, err := s.Session.Insert(table, values)
	if err != nil {
		return 0, err
	}
	// Read the full stored row back: it carries database defaults
	// (created_at and friends) the values map does not.
	row, err := readFullRow(s.Session, table, id)
	if err != nil {
		return 0, err
	}
	s.step.Created = append(s.step.Created, Row{Table: table, Values: row})
	return id, nil
}

func (s *observedSession) Delete(table string, ids ...int64) error {
	if len(ids) == 0 {
		return nil
	}
	if err := s.engine.captureTree(s.Session, s.step, table, ids); err != nil {
		return err
	}
	return s.Session.Delete(table, ids...)
}

func (s *observedSession) Update(table string, id int64, columns map[string]any) error {
	if err := s.checkColumns(table, columns); err != nil {
		return err
	}
	// Photograph the old values of exactly the declared columns first.
	old, err := readColumns(s.Session, table, id, columnNames(columns))
	if err != nil {
		return err
	}
	s.step.Updated = append(s.step.Updated, Update{Table: table, ID: id, Columns: old})
	return s.Session.Update(table, id, columns)
}

// checkColumns enforces column ownership: a command may only write the
// columns it declared. That is what keeps a send's status_code safe from
// the undo of a user edit — and vice versa.
func (s *observedSession) checkColumns(table string, columns map[string]any) error {
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

// readonlySession is handed to ReadOnly commands: any write is a
// programming error caught immediately.
type readonlySession struct {
	store.Session
}

func (s *readonlySession) Insert(table string, values map[string]any) (int64, error) {
	return 0, fmt.Errorf("a ReadOnly command tried to insert into %s", table)
}

func (s *readonlySession) Delete(table string, ids ...int64) error {
	return fmt.Errorf("a ReadOnly command tried to delete from %s", table)
}

func (s *readonlySession) Update(table string, id int64, columns map[string]any) error {
	return fmt.Errorf("a ReadOnly command tried to update %s", table)
}

// captureTree photographs every row that the cascades will destroy,
// deepest children first, so the reverse order is parents-first for
// inserts. Edges come from the manifest; their FK columns were resolved
// from the real schema at boot, so this walk never guesses.
func (e *Engine) captureTree(db store.Session, step *Step, table string, ids []int64) error {
	for _, edge := range e.manifest.edgesFor(table) {
		if !edge.Restore {
			continue // a real cascade, but undo never re-inserts it
		}
		childIDs, err := selectChildIDs(db, edge.Child, edge.FKColumn, ids)
		if err != nil {
			return err
		}
		if err := e.captureTree(db, step, edge.Child, childIDs); err != nil {
			return err
		}
	}

	rows, err := readFullRows(db, table, ids)
	if err != nil {
		return err
	}
	step.Deleted = append(step.Deleted, rows)
	return nil
}

func selectChildIDs(db store.Session, child, fkColumn string, parentIDs []int64) ([]int64, error) {
	if len(parentIDs) == 0 {
		return nil, nil
	}
	rows, err := db.Query(
		fmt.Sprintf("SELECT id FROM %s WHERE %s IN (%s)", child, fkColumn, placeholders(len(parentIDs))),
		int64Args(parentIDs)...,
	)
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

func readFullRows(db store.Session, table string, ids []int64) (RowSet, error) {
	set := RowSet{Table: table}
	if len(ids) == 0 {
		return set, nil
	}
	rows, err := db.Query(
		fmt.Sprintf("SELECT * FROM %s WHERE id IN (%s)", table, placeholders(len(ids))),
		int64Args(ids)...,
	)
	if err != nil {
		return set, fmt.Errorf("reading %s rows: %w", table, err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return set, err
	}
	idIndex := -1
	for i, column := range columns {
		if column == "id" {
			idIndex = i
			break
		}
	}
	if idIndex == -1 {
		return set, fmt.Errorf("table %s has no id column", table)
	}

	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(values))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return set, fmt.Errorf("reading a %s row: %w", table, err)
		}
		if _, ok := values[idIndex].(int64); !ok {
			return set, fmt.Errorf("table %s has a non-integer id column", table)
		}
		copied := make(map[string]any, len(columns))
		for i, column := range columns {
			copied[column] = values[i]
		}
		set.Rows = append(set.Rows, copied)
	}
	return set, rows.Err()
}

func readFullRow(db store.Session, table string, id int64) (map[string]any, error) {
	set, err := readFullRows(db, table, []int64{id})
	if err != nil {
		return nil, err
	}
	if len(set.Rows) == 0 {
		return nil, fmt.Errorf("%s #%d vanished after insert", table, id)
	}
	return set.Rows[0], nil
}

func readColumns(db store.Session, table string, id int64, columns []string) (map[string]any, error) {
	rows, err := db.Query(
		fmt.Sprintf("SELECT %s FROM %s WHERE id = ?", strings.Join(columns, ", "), table), id,
	)
	if err != nil {
		return nil, fmt.Errorf("reading %s #%d: %w", table, id, err)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, fmt.Errorf("%s #%d no longer exists", table, id)
	}
	values := make([]any, len(columns))
	pointers := make([]any, len(values))
	for i := range values {
		pointers[i] = &values[i]
	}
	if err := rows.Scan(pointers...); err != nil {
		return nil, fmt.Errorf("reading %s #%d: %w", table, id, err)
	}
	result := map[string]any{}
	for i, column := range columns {
		result[column] = values[i]
	}
	return result, rows.Err()
}

func columnNames(columns map[string]any) []string {
	names := make([]string, 0, len(columns))
	for name := range columns {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func int64Args(ids []int64) []any {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return args
}
