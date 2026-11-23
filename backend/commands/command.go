// Package commands is the app's command framework: named operations the
// UI — and one day an MCP server or CLI — invoke through one door, with
// undo for the operations that can be reversed.
//
// A command is a struct holding the transaction it was constructed with,
// plus its SQL. Undoable commands also implement Snapshot and Undo: the
// runner calls Snapshot before Run mutates anything, stores what it
// returns in the undo_log table (inside the same transaction), and hands
// it back to Undo later — together with what Run returned, since creates
// need the new id which only exists after the run.
package commands

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// Command is one operation the app can perform.
type Command interface {
	Info() Info
	Run(input json.RawMessage) (any, error)
}

// UndoableCommand is a command that can reverse itself.
type UndoableCommand interface {
	Command
	// Snapshot reads, before Run mutates anything, everything Undo will
	// need to restore the previous state.
	Snapshot(input json.RawMessage) (any, error)
	// Undo restores the previous state from the stored snapshot and
	// result.
	Undo(snapshot, result json.RawMessage) error
}

// BackupGuarded is a command that can change a lot at once, or can't be
// undone. Before it runs, the runner asks the backup guard (SetBackupGuard)
// for a backup of the whole database.
type BackupGuarded interface {
	Command
	// BackupBefore describes this run for the backup. needed=false means
	// the run is harmless (an empty collection, say). It may read the
	// database to name and size what it is about to change.
	BackupBefore(input json.RawMessage) (need BackupNeed, needed bool, err error)
}

// BackupNeed says why a backup is wanted.
type BackupNeed struct {
	Trigger string // a backup.Trigger: the setting that switches it
	Reason  string // e.g. `before Delete collection "Auth"`
	Items   int    // how many things it touches (the request-delete threshold)
}

// BackupGuard takes the backup before a guarded command. It returns finish,
// which the runner calls afterwards with whether the command committed, or
// nil when no backup was taken (switched off, too small, a recent backup).
// An error cancels the command: nothing is changed without its backup.
type BackupGuard func(need BackupNeed) (finish func(committed bool), err error)

// Info is a command's identity.
type Info struct {
	FunctionID string // "request.delete" — the reference key
	Label      string // undo-menu text
	ReadOnly   bool   // queries and sends: run without the writer lock
}

// Factory builds a command wired to the transaction it runs in.
type Factory func(tx *sql.Tx) Command

// ObservableCommand runs outside the writer lock, in parallel with other
// runs, and reports progress through the observer. It persists its own
// result in a short transaction.
type ObservableCommand interface {
	Info() Info
	Run(input json.RawMessage, obs Observer)
}

// ObservableFactory builds an observable command on the shared database.
type ObservableFactory func(db *sql.DB) ObservableCommand

// Observer is the callback set an observable command reports through:
// Update for progress, Finish (exactly once) for the final result.
type Observer struct {
	Update func(data any)
	Finish func(result any, err error)
	// Context is cancelled by CommandService.Cancel.
	Context context.Context
}

// Update is a progress event from an observable command.
type Update struct {
	RunID   string `json:"runId"`
	Command string `json:"command"`
	Data    any    `json:"data"`
}

// Finish is the completion event from an observable command.
type Finish struct {
	RunID   string `json:"runId"`
	Command string `json:"command"`
	Result  any    `json:"result,omitempty"`
	Error   string `json:"error,omitempty"`
}

// CommandService is the single Wails door to the framework: run commands
// by name, undo, redo, and inspect the undo list.
type CommandService struct {
	runner *Runner
}

// NewCommandService returns a CommandService backed by the given runner.
func NewCommandService(runner *Runner) *CommandService {
	return &CommandService{runner: runner}
}

// CommandExecuteResult carries a command's marshalled result.
type CommandExecuteResult struct {
	Result json.RawMessage `json:"result"`
}

// Execute runs a named command with a JSON payload and returns the
// marshalled result.
func (s *CommandService) Execute(name string, payload string) (CommandExecuteResult, error) {
	out, err := s.runner.Run(name, json.RawMessage(payload))
	if err != nil {
		return CommandExecuteResult{}, err
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return CommandExecuteResult{}, err
	}
	return CommandExecuteResult{Result: raw}, nil
}

// Begin starts an observable command and returns its run id immediately.
// Progress and completion arrive as command:update / command:finish events.
func (s *CommandService) Begin(name string, payload string) (string, error) {
	return s.runner.Begin(name, json.RawMessage(payload))
}

// Cancel stops a run started with Begin. It still finishes, with an error.
func (s *CommandService) Cancel(runID string) { s.runner.Cancel(runID) }

// Undo reverses the newest undoable run and reports what happened.
func (s *CommandService) Undo() (UndoResult, error) { return s.runner.Undo() }

// Redo repeats the newest undone run.
func (s *CommandService) Redo() (UndoResult, error) { return s.runner.Redo() }

// History returns the current undo-menu summary.
func (s *CommandService) History() Status { return s.runner.Status() }

// Stack lists every undo log row, oldest first, for the inspector panel.
func (s *CommandService) Stack() []StepInfo { return s.runner.Steps() }

// ListCommands returns the catalog, for callers that ask "what can this
// app do?".
func (s *CommandService) ListCommands() []CommandInfo { return s.runner.ListCommands() }

// CommandInfo is one entry of the catalog.
type CommandInfo struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Undoable bool   `json:"undoable"`
	ReadOnly bool   `json:"readOnly"`
}

var _ = fmt.Sprintf
var _ = errors.New
