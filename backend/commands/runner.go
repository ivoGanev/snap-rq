package commands

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// The runner is deliberately dumb: it looks up the command, opens the
// transaction, calls Snapshot (undoable commands only), calls Run, writes
// the undo log row inside the same transaction, and commits. Undo hands
// the stored snapshot and result back to the command's Undo. Redo re-runs
// the command from its recorded input. It never inspects or reverses any
// data itself.

var (
	// ErrNothingToUndo is returned by Undo on an empty log.
	ErrNothingToUndo = errors.New("nothing to undo")
	// ErrNothingToRedo is returned by Redo on an empty redo list.
	ErrNothingToRedo = errors.New("nothing to redo")
)

const (
	// EventChanged fires after every write command, carrying the command
	// name; the frontend refreshes the matching entity.
	EventChanged = "command:changed"
	// EventHistory fires whenever the undo log changes.
	EventHistory = "command:history"

	// mergeWindow is how long quick repeats of the same command keep one
	// undo step (typing a name is many runs of request.update on one row).
	mergeWindow = 750 * time.Millisecond
)

type Runner struct {
	db        *sql.DB
	factories map[string]Factory
	infos     map[string]Info
	undoable  map[string]bool

	mu         sync.Mutex
	limit      func() int
	lastAction string // "run", "undo" or "redo" — drives the undo toast
	lastLabel  string
}

// NewRunner returns a runner for the given database.
func NewRunner(db *sql.DB) *Runner {
	return &Runner{
		db:        db,
		factories: map[string]Factory{},
		infos:     map[string]Info{},
		undoable:  map[string]bool{},
	}
}

// Register makes commands runnable by name.
func (r *Runner) Register(factories ...Factory) error {
	for _, factory := range factories {
		cmd := factory(nil)
		info := cmd.Info()
		if info.FunctionID == "" {
			return errors.New("a command has no FunctionID")
		}
		if _, exists := r.factories[info.FunctionID]; exists {
			return fmt.Errorf("command %s is already registered", info.FunctionID)
		}
		r.factories[info.FunctionID] = factory
		r.infos[info.FunctionID] = info
		_, r.undoable[info.FunctionID] = cmd.(UndoableCommand)
		if r.undoable[info.FunctionID] {
			if _, ok := cmd.(ReportsChanges); !ok {
				return fmt.Errorf("command %s is undoable but does not report its changes", info.FunctionID)
			}
		}
	}
	return nil
}

// SetUndoLimit installs the settings-backed undo log size.
func (r *Runner) SetUndoLimit(fn func() int) { r.limit = fn }

func (r *Runner) undoLimit() int {
	if r.limit != nil {
		if n := r.limit(); n > 0 {
			return n
		}
	}
	return 100
}

// event is one Wails event, sent after the lock is released.
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

// Run executes a command by name.
func (r *Runner) Run(name string, input json.RawMessage) (any, error) {
	factory, ok := r.factories[name]
	if !ok {
		return nil, fmt.Errorf("unknown command %q", name)
	}
	info := r.infos[name]

	// Read-only commands (queries, sends) run without the writer lock, so
	// a slow send never blocks a rename; the transaction is rolled back,
	// discarding anything a read-only command should not have written.
	if info.ReadOnly {
		tx, err := r.db.Begin()
		if err != nil {
			return nil, err
		}
		defer tx.Rollback()
		return factory(tx).Run(input)
	}

	r.mu.Lock()
	result, events, err := r.runWrite(name, info, factory, input)
	r.mu.Unlock()
	emit(events)
	return result, err
}

func (r *Runner) runWrite(name string, info Info, factory Factory, input json.RawMessage) (any, []event, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	cmd := factory(tx)

	var snapshot string
	if r.undoable[name] {
		saved, err := cmd.(UndoableCommand).Snapshot(input)
		if err != nil {
			return nil, nil, err
		}
		raw, err := json.Marshal(saved)
		if err != nil {
			return nil, nil, err
		}
		snapshot = string(raw)
	}

	result, err := cmd.Run(input)
	if err != nil {
		return nil, nil, err
	}

	reporter, reports := cmd.(ReportsChanges)
	var resultRaw []byte
	if reports || r.undoable[name] {
		resultRaw, _ = json.Marshal(result)
	}

	var events []event
	if reports {
		events = append(events, event{EventChanged, changedEvent(name, reporter.Changes(input, resultRaw))})
	}
	if r.undoable[name] {
		if err := r.recordUndo(tx, name, info.Label, string(input), snapshot, string(resultRaw)); err != nil {
			return nil, nil, err
		}
		r.lastAction, r.lastLabel = "run", info.Label
		events = append(events, event{EventHistory, r.status()})
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return result, events, nil
}

// recordUndo stores how to reverse this run — inside the run's own
// transaction, so a committed change can never lack its undo row. Quick
// repeats of the same command keep one step: the older snapshot stays
// (undo goes back to where the typing started), the newer input is kept
// for redo.
func (r *Runner) recordUndo(tx *sql.Tx, name, label, input, snapshot, result string) error {
	var prevID int64
	var prevCommand, prevInput string
	var prevCreatedAt int64
	err := tx.QueryRow(
		`SELECT id, command, input, created_at FROM undo_log WHERE state = 'undo' ORDER BY id DESC LIMIT 1`,
	).Scan(&prevID, &prevCommand, &prevInput, &prevCreatedAt)
	// merge only quick repeats of the same command on the same rows (the
	// id or ids field of the input identifies the rows)
	if err == nil && prevCommand == name && sameRows(prevInput, input) &&
		time.Since(time.UnixMilli(prevCreatedAt)) < mergeWindow {
		_, err = tx.Exec(
			`UPDATE undo_log SET input = ?, result = ?, created_at = ? WHERE id = ?`,
			input, result, time.Now().UnixMilli(), prevID,
		)
	} else {
		_, err = tx.Exec(
			`INSERT INTO undo_log (command, label, input, snapshot, result, created_at, state)
			 VALUES (?, ?, ?, ?, ?, ?, 'undo')`,
			name, label, input, snapshot, result, time.Now().UnixMilli(),
		)
	}
	if err != nil {
		return err
	}

	// a new undoable change makes redo impossible
	if _, err := tx.Exec(`DELETE FROM undo_log WHERE state = 'redo'`); err != nil {
		return err
	}
	// trim to the limit, oldest first
	_, err = tx.Exec(
		`DELETE FROM undo_log WHERE state = 'undo' AND id NOT IN
		 (SELECT id FROM undo_log WHERE state = 'undo' ORDER BY id DESC LIMIT ?)`,
		r.undoLimit(),
	)
	return err
}

// Undo reverses the newest undoable run. An Undo error means the data is
// gone for good or the state has diverged: the step is dropped with a
// notice and the next one is tried.
func (r *Runner) Undo() (UndoResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var skipped []string
	for {
		row := r.newestRow("undo")
		if row == nil {
			return UndoResult{Skipped: skipped}, ErrNothingToUndo
		}

		tx, err := r.db.Begin()
		if err != nil {
			return UndoResult{Skipped: skipped}, err
		}
		cmd := r.factories[row.Command](tx)
		undoable, ok := cmd.(UndoableCommand)
		if !ok {
			tx.Rollback()
			r.deleteRow(row.ID)
			continue
		}

		if err := undoable.Undo([]byte(row.Snapshot), []byte(row.Result)); err != nil {
			tx.Rollback()
			r.deleteRow(row.ID)
			skipped = append(skipped, fmt.Sprintf("Skipped %q: %v", row.Label, err))
			continue
		}
		if _, err := tx.Exec(`UPDATE undo_log SET state = 'redo' WHERE id = ?`, row.ID); err != nil {
			tx.Rollback()
			return UndoResult{Skipped: skipped}, err
		}
		if err := tx.Commit(); err != nil {
			return UndoResult{Skipped: skipped}, err
		}

		r.lastAction, r.lastLabel = "undo", row.Label
		emit([]event{
			{EventChanged, changedEvent(row.Command, changesOf(cmd, row.Input, row.Result))},
			{EventHistory, r.status()},
		})
		return UndoResult{Command: row.Command, Label: row.Label, Skipped: skipped}, nil
	}
}

// Redo re-runs the newest undone run from its recorded input; the re-run's
// own Snapshot and log row replace the old one.
func (r *Runner) Redo() (UndoResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	row := r.newestRow("redo")
	if row == nil {
		return UndoResult{}, ErrNothingToRedo
	}

	tx, err := r.db.Begin()
	if err != nil {
		return UndoResult{}, err
	}
	defer tx.Rollback()

	cmd := r.factories[row.Command](tx)

	var snapshot string
	if r.undoable[row.Command] {
		saved, err := cmd.(UndoableCommand).Snapshot([]byte(row.Input))
		if err != nil {
			return UndoResult{}, err
		}
		raw, _ := json.Marshal(saved)
		snapshot = string(raw)
	}
	result, err := cmd.Run([]byte(row.Input))
	if err != nil {
		return UndoResult{}, err
	}
	resultRaw, _ := json.Marshal(result)
	if err := r.recordUndo(tx, row.Command, row.Label, row.Input, snapshot, string(resultRaw)); err != nil {
		return UndoResult{}, err
	}
	if _, err := tx.Exec(`DELETE FROM undo_log WHERE id = ?`, row.ID); err != nil {
		return UndoResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return UndoResult{}, err
	}

	r.lastAction, r.lastLabel = "redo", row.Label
	emit([]event{
		{EventChanged, changedEvent(row.Command, changesOf(cmd, row.Input, string(resultRaw)))},
		{EventHistory, r.status()},
	})
	return UndoResult{Command: row.Command, Label: row.Label}, nil
}

// Status is the summary the undo menu keeps on screen.
type Status struct {
	UndoSteps     int    `json:"undoSteps"`
	RedoSteps     int    `json:"redoSteps"`
	NextUndoLabel string `json:"nextUndoLabel"`
	NextRedoLabel string `json:"nextRedoLabel"`
	LastAction    string `json:"lastAction"`
	LastLabel     string `json:"lastLabel"`
}

// Status returns the current summary.
func (r *Runner) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status()
}

func (r *Runner) status() Status {
	status := Status{LastAction: r.lastAction, LastLabel: r.lastLabel}
	r.db.QueryRow(`SELECT COUNT(*) FROM undo_log WHERE state = 'undo'`).Scan(&status.UndoSteps)
	r.db.QueryRow(`SELECT COUNT(*) FROM undo_log WHERE state = 'redo'`).Scan(&status.RedoSteps)
	if row := r.newestRow("undo"); row != nil {
		status.NextUndoLabel = row.Label
	}
	if row := r.newestRow("redo"); row != nil {
		status.NextRedoLabel = row.Label
	}
	return status
}

// StepInfo describes one undo log row for the inspector panel.
type StepInfo struct {
	Seq     int    `json:"seq"`
	Command string `json:"command"`
	Label   string `json:"label"`
	At      string `json:"at"`
	State   string `json:"state"`
	Rows    string `json:"rows"`
}

// Steps lists every undo log row, oldest first.
func (r *Runner) Steps() []StepInfo {
	r.mu.Lock()
	defer r.mu.Unlock()

	rows, err := r.db.Query(`SELECT id, command, label, input, created_at, state FROM undo_log ORDER BY id`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var infos []StepInfo
	seq := 0
	for rows.Next() {
		var id int64
		var command, label, input, state string
		var createdAt int64
		if err := rows.Scan(&id, &command, &label, &input, &createdAt, &state); err != nil {
			return nil
		}
		rowsText := input
		if len(rowsText) > 80 {
			rowsText = rowsText[:80] + "…"
		}
		infos = append(infos, StepInfo{
			Seq:     seq,
			Command: command,
			Label:   label,
			At:      time.UnixMilli(createdAt).UTC().Format("2006-01-02 15:04:05"),
			State:   state,
			Rows:    rowsText,
		})
		seq++
	}
	return infos
}

// ListCommands returns the catalog.
func (r *Runner) ListCommands() []CommandInfo {
	infos := make([]CommandInfo, 0, len(r.infos))
	for name, info := range r.infos {
		infos = append(infos, CommandInfo{
			Name:     name,
			Label:    info.Label,
			Undoable: r.undoable[name],
			ReadOnly: info.ReadOnly,
		})
	}
	return infos
}

// CommandChanged is the payload of EventChanged: the operation plus the
// rows it changed, so the frontend refreshes exactly those.
type CommandChanged struct {
	Operation string      `json:"operation"`
	Changes   []RowChange `json:"changes"`
}

// changesOf asks the command which rows it changed, from the same input and
// result the run is logged with.
func changesOf(cmd Command, input, result string) []RowChange {
	if reporter, ok := cmd.(ReportsChanges); ok {
		return reporter.Changes([]byte(input), []byte(result))
	}
	return nil
}

// changedEvent splits the command name ("request.delete") into its
// operation.
func changedEvent(name string, changes []RowChange) CommandChanged {
	_, operation, _ := strings.Cut(name, ".")
	return CommandChanged{Operation: operation, Changes: changes}
}

// UndoResult reports what an Undo or Redo did.
type UndoResult struct {
	Command string `json:"command"`
	Label   string `json:"label"`
	// Skipped lists steps that could not be undone because their rows were
	// destroyed for good.
	Skipped []string `json:"skipped,omitempty"`
}

func (r *Runner) newestRow(state string) *undoRow {
	var row undoRow
	err := r.db.QueryRow(
		`SELECT id, command, label, input, snapshot, result, created_at, state
		 FROM undo_log WHERE state = ? ORDER BY id DESC LIMIT 1`, state,
	).Scan(&row.ID, &row.Command, &row.Label, &row.Input, &row.Snapshot, &row.Result, &row.CreatedAt, &row.State)
	if err != nil {
		return nil
	}
	return &row
}

func (r *Runner) deleteRow(id int64) {
	r.db.Exec(`DELETE FROM undo_log WHERE id = ?`, id)
}

// sameRows reports whether two inputs target the same rows: the id or ids
// field must match, everything else (like the new name) may differ.
func sameRows(a, b string) bool {
	var aMap, bMap map[string]any
	if json.Unmarshal([]byte(a), &aMap) != nil || json.Unmarshal([]byte(b), &bMap) != nil {
		return a == b
	}
	return fmt.Sprint(aMap["id"]) == fmt.Sprint(bMap["id"]) &&
		fmt.Sprint(aMap["ids"]) == fmt.Sprint(bMap["ids"]) &&
		fmt.Sprint(aMap["request_id"]) == fmt.Sprint(bMap["request_id"]) &&
		fmt.Sprint(aMap["collection_id"]) == fmt.Sprint(bMap["collection_id"])
}

type undoRow struct {
	ID        int64
	Command   string
	Label     string
	Input     string
	Snapshot  string
	Result    string
	CreatedAt int64
	State     string
}
