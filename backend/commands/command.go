// Package commands is the app's command framework: the operations the app
// can perform (invoked by name from the UI, and one day from an MCP server
// or CLI), the engine that runs them, and the undo store that reverses
// the ones worth reversing.
//
// A command is a stateless type implementing Run. Everything it needs
// arrives via the database session and its JSON input.
package commands

import (
	"encoding/json"

	"snap-rq/backend/store"
)

// Command is one operation the app can perform.
type Command interface {
	// Info identifies the command and names the operation that reverses it.
	Info() Info
	// Run performs the operation. Reads use db.Query/QueryRow freely;
	// writes go through db.Insert/Delete/Update so the engine can observe
	// them (raw db.Exec is for system commands that are never undone).
	Run(db store.Session, input json.RawMessage) (any, error)
}

// Info is a command's identity.
type Info struct {
	// FunctionID is the reference key, e.g. "request.create".
	FunctionID string
	// Label is the human name, shown in the undo menu.
	Label string
	// UndoOperationID names the command that reverses this one — for the
	// catalog, boot validation and conformance tests. "" = the command is
	// not undoable (queries, sends, history writes). Undo itself replays
	// recorded rows; it does not run that command.
	UndoOperationID string
	// ReadOnly marks commands that change nothing in the database: they
	// run without the writer lock and without a transaction, so a slow
	// call (a request send with a long timeout) never blocks anything.
	ReadOnly bool
	// Columns lists the columns this command may write. Undo writes back
	// exactly these and nothing else — a send's status update can never be
	// clobbered by the undo of a user edit.
	Columns []string
}

// A command may implement Undoer to reverse its own steps with hand-written
// SQL (one bulk statement, derived semantics) instead of the engine's
// default row replay. Redoer is optional for the same reason; without it,
// redo re-runs the command from its recorded input.
type Undoer interface {
	Undo(db store.Session, s *Step) error
}

type Redoer interface {
	Redo(db store.Session, s *Step) error
}

// A command whose writes are one bulk statement implements Capturer so the
// engine can save the current values of the rows it will touch first: it
// maps table names to the row ids its input will change.
type Capturer interface {
	CaptureRows(input json.RawMessage) map[string][]int64
}

// A command that destroys rows for good implements Forgetter: after it
// commits, the engine drops every undo step involving those rows, so undo
// can never bring back something the user meant to destroy. It receives
// the command's Run result.
type Forgetter interface {
	ForgetFrom(result any) map[string][]int64
}

// CommandService is the single Wails door to the framework: run commands
// by name, undo, redo, and inspect the undo list. The frontend, and later
// an MCP server or CLI, share it.
type CommandService struct {
	engine *Engine
}

// NewCommandService returns a CommandService backed by the given engine.
func NewCommandService(engine *Engine) *CommandService {
	return &CommandService{engine: engine}
}

// CommandExecuteResult carries a command's marshalled result.
type CommandExecuteResult struct {
	Result json.RawMessage `json:"result"`
}

// Execute runs a named command with a JSON payload and returns the
// marshalled result.
func (s *CommandService) Execute(name string, payload string) (CommandExecuteResult, error) {
	out, err := s.engine.Run(name, json.RawMessage(payload))
	if err != nil {
		return CommandExecuteResult{}, err
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return CommandExecuteResult{}, err
	}
	return CommandExecuteResult{Result: raw}, nil
}

// Undo reverses the newest undoable step and reports what happened.
func (s *CommandService) Undo() (UndoResult, error) {
	return s.engine.Undo()
}

// Redo re-applies the newest undone step.
func (s *CommandService) Redo() (UndoResult, error) {
	return s.engine.Redo()
}

// History returns the current summary for the undo menu.
func (s *CommandService) History() Status {
	return s.engine.Status()
}

// Stack lists every remembered step, oldest first, for the inspector panel.
func (s *CommandService) Stack() []StepInfo {
	return s.engine.Steps()
}

// ListCommands returns the catalog — for callers that ask "what can this
// app do?".
func (s *CommandService) ListCommands() []CommandInfo {
	return s.engine.ListCommands()
}
