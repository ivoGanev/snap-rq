package services

import (
	"encoding/json"

	"snap-rq/backend/commands"
)

// CommandService exposes the command history to the frontend: running
// commands by name, undo/redo, the status for the undo menu, the step list
// for the inspector panel, and the list of known commands for future
// external callers.
type CommandService struct {
	history *commands.History
}

// NewCommandService returns a CommandService backed by the given history.
func NewCommandService(history *commands.History) *CommandService {
	return &CommandService{history: history}
}

// CommandExecuteResult carries a command's marshalled result.
type CommandExecuteResult struct {
	Result json.RawMessage `json:"result"`
}

// Execute runs a named command with a JSON payload and returns the
// marshalled result. This is the entry point future external callers share
// with the UI.
func (s *CommandService) Execute(name string, payload string) (CommandExecuteResult, error) {
	out, err := s.history.Run(name, json.RawMessage(payload))
	if err != nil {
		return CommandExecuteResult{}, err
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return CommandExecuteResult{}, err
	}
	return CommandExecuteResult{Result: raw}, nil
}

// Undo puts the newest remembered step back and reports what happened.
func (s *CommandService) Undo() (commands.UndoResult, error) {
	return s.history.Undo()
}

// Redo puts the newest undone step back again.
func (s *CommandService) Redo() (commands.UndoResult, error) {
	return s.history.Redo()
}

// History returns the current summary for the undo menu.
func (s *CommandService) History() commands.Status {
	return s.history.Status()
}

// Stack lists every remembered step, oldest first, for the inspector
// panel.
func (s *CommandService) Stack() []commands.StepInfo {
	return s.history.Steps()
}

// ListCommands returns the known commands, for callers that ask "what can
// this app do?".
func (s *CommandService) ListCommands() []commands.CommandInfo {
	return s.history.KnownCommands()
}
