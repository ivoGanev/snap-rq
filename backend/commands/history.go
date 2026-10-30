package commands

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	// DefaultUndoLimit bounds the undo list when no settings function is
	// installed. The user can change it in settings.
	DefaultUndoLimit = 100

	// mergeWindow is how long quick repeats of the same edit are merged
	// into one undo step.
	mergeWindow = 750 * time.Millisecond

	// EventChanged fires after every command, telling the UI which
	// entities to refresh (ids only, never values).
	EventChanged = "command:changed"
	// EventHistory fires whenever the undo list changes.
	EventHistory = "command:history"
)

// ErrNothingToUndo is returned by Undo on an empty list.
var ErrNothingToUndo = errors.New("nothing to undo")

// ErrNothingToRedo is returned by Redo on an empty list.
var ErrNothingToRedo = errors.New("nothing to redo")

// History runs every change the app makes and remembers how to undo the
// ones worth remembering. It is the app's only writer: commands run one at
// a time, each inside its own transaction.
type History struct {
	db *sql.DB
	mu sync.Mutex

	known map[string]*Command

	undoSteps []*UndoStep
	redoSteps []*UndoStep

	// undoLimit is read through a function so the user can change it in
	// settings while the app runs.
	undoLimit func() int

	lastAction string // "run", "undo" or "redo" — drives the undo toast
	lastLabel  string
}

// NewHistory returns an empty History for the given database.
func NewHistory(db *sql.DB) *History {
	return &History{
		db:    db,
		known: map[string]*Command{},
	}
}

// Register makes a command runnable by name.
func (h *History) Register(cmd *Command) error {
	if cmd.Name == "" {
		return errors.New("command name is required")
	}
	if cmd.Apply == nil {
		return fmt.Errorf("command %s has no Apply function", cmd.Name)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.known[cmd.Name]; exists {
		return fmt.Errorf("command %s is already registered", cmd.Name)
	}
	h.known[cmd.Name] = cmd
	return nil
}

// SetUndoLimit installs the settings-backed undo list size. Missing or
// non-positive values fall back to DefaultUndoLimit.
func (h *History) SetUndoLimit(fn func() int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.undoLimit = fn
}

func (h *History) limit() int {
	if h.undoLimit != nil {
		if n := h.undoLimit(); n > 0 {
			return n
		}
	}
	return DefaultUndoLimit
}

// UndoStep is one remembered change: copies of every row it touched,
// before and after it ran.
type UndoStep struct {
	Command  string    // e.g. "request.delete"
	Label    string    // e.g. "Delete Request"
	When     time.Time // when it ran
	MergeKey string    // non-empty when quick repeats merge into one step
	Before   []SavedRow
	After    []SavedRow
	Touched  []Touched
}

// refs groups the touched ids per entity, for the UI to restore selection.
func (s *UndoStep) refs() map[string][]int64 {
	refs := make(map[string][]int64, len(s.Touched))
	for _, touched := range s.Touched {
		refs[touched.Entity] = append(refs[touched.Entity], touched.IDs...)
	}
	return refs
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

// CommandChanged is the payload of EventChanged.
type CommandChanged struct {
	Command string    `json:"command"`
	Changes []Touched `json:"changes"`
}

// Run executes a command by name.
func (h *History) Run(name string, input json.RawMessage) (any, error) {
	h.mu.Lock()
	result, events, err := h.runLocked(name, input)
	h.mu.Unlock()
	emit(events)
	return result, err
}

func (h *History) runLocked(name string, input json.RawMessage) (any, []event, error) {
	cmd, ok := h.known[name]
	if !ok {
		return nil, nil, fmt.Errorf("unknown command %q", name)
	}

	tx, err := h.db.Begin()
	if err != nil {
		return nil, nil, fmt.Errorf("starting the transaction: %w", err)
	}
	defer tx.Rollback()

	rec := newRecorder(tx)
	result, err := cmd.Apply(tx, rec, input)
	if err != nil {
		return nil, nil, err
	}

	if cmd.Undoable {
		if err := rec.saveAfterCopies(); err != nil {
			return nil, nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("committing: %w", err)
	}
	if cmd.Undoable {
		h.remember(cmd, input, rec)
		h.lastAction, h.lastLabel = "run", cmd.Label
	}

	events := []event{
		{EventChanged, CommandChanged{Command: name, Changes: rec.touched}},
		{EventHistory, h.statusLocked()},
	}
	return result, events, nil
}

// remember adds a finished command to the undo list, merging it into the
// previous step when it is a quick repeat of the same edit.
func (h *History) remember(cmd *Command, input json.RawMessage, rec *Recorder) {
	key := ""
	if cmd.MergeKey != nil {
		key = cmd.MergeKey(input)
	}

	if key != "" && len(h.undoSteps) > 0 {
		last := h.undoSteps[len(h.undoSteps)-1]
		if last.MergeKey == key && time.Since(last.When) < mergeWindow {
			// Keep the oldest copies: one undo goes back to where the
			// typing started, not to the middle of it.
			last.After = rec.step(cmd, key).After
			last.When = time.Now()
			last.Touched = rec.touched
			return
		}
	}

	// A new change makes redo impossible: it happens in a world the redo
	// steps know nothing about.
	h.redoSteps = nil
	h.undoSteps = append(h.undoSteps, rec.step(cmd, key))

	for len(h.undoSteps) > h.limit() {
		h.undoSteps = h.undoSteps[1:]
	}
}

// UndoResult reports what an Undo or Redo did.
type UndoResult struct {
	Command string             `json:"command"`
	Label   string             `json:"label"`
	Refs    map[string][]int64 `json:"refs"`
	// Skipped lists steps that could not be undone because their rows were
	// destroyed for good (e.g. the trash was emptied permanently).
	Skipped []string `json:"skipped,omitempty"`
}

// Undo puts the before-copies of the newest step back.
func (h *History) Undo() (UndoResult, error) {
	h.mu.Lock()
	result, events, err := h.undoLocked()
	h.mu.Unlock()
	emit(events)
	return result, err
}

func (h *History) undoLocked() (UndoResult, []event, error) {
	var skipped []string
	for len(h.undoSteps) > 0 {
		step := h.undoSteps[len(h.undoSteps)-1]
		h.undoSteps = h.undoSteps[:len(h.undoSteps)-1]

		if err := rowsStillExist(h.db, step); err != nil {
			// The rows are gone for good; undoing this step is
			// impossible. Forget it and try the next one.
			skipped = append(skipped, fmt.Sprintf("Skipped %q: %v", step.Label, err))
			continue
		}

		if err := putRowsBack(h.db, step.Before, step.After); err != nil {
			// Nothing happened (the restore is one transaction), so the
			// step goes back on the list and the error is shown.
			h.undoSteps = append(h.undoSteps, step)
			return UndoResult{Skipped: skipped}, nil, fmt.Errorf("cannot undo %s: %w", step.Command, err)
		}

		h.redoSteps = append(h.redoSteps, step)
		h.lastAction, h.lastLabel = "undo", step.Label
		events := []event{
			{EventChanged, CommandChanged{Command: step.Command, Changes: step.Touched}},
			{EventHistory, h.statusLocked()},
		}
		return UndoResult{
			Command: step.Command,
			Label:   step.Label,
			Refs:    step.refs(),
			Skipped: skipped,
		}, events, nil
	}
	return UndoResult{Skipped: skipped}, nil, ErrNothingToUndo
}

// Redo puts the after-copies of the newest undone step back.
func (h *History) Redo() (UndoResult, error) {
	h.mu.Lock()
	result, events, err := h.redoLocked()
	h.mu.Unlock()
	emit(events)
	return result, err
}

func (h *History) redoLocked() (UndoResult, []event, error) {
	if len(h.redoSteps) == 0 {
		return UndoResult{}, nil, ErrNothingToRedo
	}

	step := h.redoSteps[len(h.redoSteps)-1]
	h.redoSteps = h.redoSteps[:len(h.redoSteps)-1]

	if err := putRowsBack(h.db, step.After, step.Before); err != nil {
		h.redoSteps = append(h.redoSteps, step)
		return UndoResult{}, nil, fmt.Errorf("cannot redo %s: %w", step.Command, err)
	}

	h.undoSteps = append(h.undoSteps, step)
	h.lastAction, h.lastLabel = "redo", step.Label
	events := []event{
		{EventChanged, CommandChanged{Command: step.Command, Changes: step.Touched}},
		{EventHistory, h.statusLocked()},
	}
	return UndoResult{
		Command: step.Command,
		Label:   step.Label,
		Refs:    step.refs(),
	}, events, nil
}

// Status is the summary the undo menu keeps on screen.
type Status struct {
	UndoSteps     int    `json:"undoSteps"`
	RedoSteps     int    `json:"redoSteps"`
	NextUndoLabel string `json:"nextUndoLabel"`
	NextRedoLabel string `json:"nextRedoLabel"`
	LastAction    string `json:"lastAction"` // "run", "undo" or "redo"
	LastLabel     string `json:"lastLabel"`
}

// Status returns the current summary.
func (h *History) Status() Status {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.statusLocked()
}

func (h *History) statusLocked() Status {
	status := Status{
		UndoSteps:  len(h.undoSteps),
		RedoSteps:  len(h.redoSteps),
		LastAction: h.lastAction,
		LastLabel:  h.lastLabel,
	}
	if len(h.undoSteps) > 0 {
		status.NextUndoLabel = h.undoSteps[len(h.undoSteps)-1].Label
	}
	if len(h.redoSteps) > 0 {
		status.NextRedoLabel = h.redoSteps[len(h.redoSteps)-1].Label
	}
	return status
}

// StepInfo describes one step for the undo inspector panel.
type StepInfo struct {
	Seq     int                `json:"seq"`
	Command string             `json:"command"`
	Label   string             `json:"label"`
	At      string             `json:"at"`
	State   string             `json:"state"` // "undo" or "redo"
	Tables  map[string][]int64 `json:"tables"`
}

// Steps lists every remembered step, oldest first, for the inspector panel.
func (h *History) Steps() []StepInfo {
	h.mu.Lock()
	defer h.mu.Unlock()

	infos := make([]StepInfo, 0, len(h.undoSteps)+len(h.redoSteps))
	for i, step := range h.undoSteps {
		infos = append(infos, stepInfo(step, i, "undo"))
	}
	for i, step := range h.redoSteps {
		infos = append(infos, stepInfo(step, len(h.undoSteps)+i, "redo"))
	}
	return infos
}

func stepInfo(step *UndoStep, seq int, state string) StepInfo {
	// One line per table: which row ids the step touched there.
	tables := make(map[string][]int64, len(step.Before)+len(step.After))
	for _, row := range step.Before {
		tables[row.Table] = append(tables[row.Table], row.ID)
	}
	for _, row := range step.After {
		if !containsID(tables[row.Table], row.ID) {
			tables[row.Table] = append(tables[row.Table], row.ID)
		}
	}
	return StepInfo{
		Seq:     seq,
		Command: step.Command,
		Label:   step.Label,
		At:      step.When.UTC().Format("2006-01-02 15:04:05"),
		State:   state,
		Tables:  tables,
	}
}

func containsID(ids []int64, id int64) bool {
	for _, existing := range ids {
		if existing == id {
			return true
		}
	}
	return false
}

// CommandInfo is one entry of the list of known commands, for callers that
// ask "what can this app do?".
type CommandInfo struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Undoable bool   `json:"undoable"`
}

// KnownCommands lists the registered commands.
func (h *History) KnownCommands() []CommandInfo {
	h.mu.Lock()
	defer h.mu.Unlock()

	infos := make([]CommandInfo, 0, len(h.known))
	for _, cmd := range h.known {
		infos = append(infos, CommandInfo{Name: cmd.Name, Label: cmd.Label, Undoable: cmd.Undoable})
	}
	return infos
}

// ForgetRows throws away every undo step that involves the given rows.
// Used when the user destroys data for good (emptying the trash): undo
// must never bring back something the user meant to destroy.
func (h *History) ForgetRows(table string, ids ...int64) {
	if len(ids) == 0 {
		return
	}
	involved := make(map[int64]bool, len(ids))
	for _, id := range ids {
		involved[id] = true
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	keep := func(steps []*UndoStep) []*UndoStep {
		kept := steps[:0]
		for _, step := range steps {
			if !stepTouches(step, table, involved) {
				kept = append(kept, step)
			}
		}
		return kept
	}
	h.undoSteps = keep(h.undoSteps)
	h.redoSteps = keep(h.redoSteps)
}

func stepTouches(step *UndoStep, table string, ids map[int64]bool) bool {
	for _, row := range step.Before {
		if row.Table == table && ids[row.ID] {
			return true
		}
	}
	for _, row := range step.After {
		if row.Table == table && ids[row.ID] {
			return true
		}
	}
	return false
}
