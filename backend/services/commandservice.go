package services

import (
	"encoding/json"

	"snap-rq/backend/commands"
)

// CommandService exposes the command bus to the frontend: named command
// execution, undo/redo, history for UI labels, the live stack for the
// development panel, and the command registry for future external callers.
type CommandService struct {
	bus *commands.Bus
}

// NewCommandService returns a CommandService backed by the given bus.
func NewCommandService(bus *commands.Bus) *CommandService {
	return &CommandService{bus: bus}
}

// CommandExecuteResult carries a command's marshalled result.
type CommandExecuteResult struct {
	Result json.RawMessage `json:"result"`
}

// Execute runs a named command with a JSON payload and returns the
// marshalled result. This is the entry point future external callers share
// with the UI.
func (s *CommandService) Execute(name string, payload string) (CommandExecuteResult, error) {
	out, err := s.bus.Execute(name, json.RawMessage(payload))
	if err != nil {
		return CommandExecuteResult{}, err
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return CommandExecuteResult{}, err
	}
	return CommandExecuteResult{Result: raw}, nil
}

// Undo applies the newest undo entry and reports what happened, including
// refs for UI-state restoration and notices for skipped stale entries.
func (s *CommandService) Undo() (commands.UndoResult, error) {
	return s.bus.Undo()
}

// Redo re-applies the newest undone entry.
func (s *CommandService) Redo() (commands.UndoResult, error) {
	return s.bus.Redo()
}

// History returns the current stack summary for UI labels and toasts.
func (s *CommandService) History() commands.HistorySnapshot {
	return s.bus.History()
}

// Stack lists every live undo/redo entry, oldest first, for the development
// panel.
func (s *CommandService) Stack() []commands.StackEntryInfo {
	return s.bus.Stack()
}

// ListCommands returns the command registry (self-describing for future
// external callers).
func (s *CommandService) ListCommands() []commands.CommandInfo {
	return s.bus.ListCommands()
}
