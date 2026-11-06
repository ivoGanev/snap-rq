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
