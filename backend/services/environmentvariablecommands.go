package services

import (
	"encoding/json"
	"errors"
	"fmt"

	"snap-rq/backend/commands"
)

// EnvironmentVariableDeleteCommandName is the public command name for
// deleting a single environment variable (see specs/command-framework.md).
const EnvironmentVariableDeleteCommandName = "environment_variable.delete"

// RegisterCommands registers this service's commands on the bus and routes
// this service's mutations through it.
func (s *EnvironmentVariableService) RegisterCommands(bus *commands.Bus) error {
	s.bus = bus
	return bus.Register(s.deleteCommand())
}

// deleteCommand removes one variable as an undoable entry. Undo restores the
// exact key/value row under its original id.
func (s *EnvironmentVariableService) deleteCommand() *commands.Command {
	return &commands.Command{
		Name:     EnvironmentVariableDeleteCommandName,
		Label:    "Delete Variable",
		Category: commands.Undoable,
		Handler: func(ctx *commands.Context, payload json.RawMessage) (any, error) {
			var pl struct {
				ID int64 `json:"id"`
			}
			if err := json.Unmarshal(payload, &pl); err != nil {
				return nil, err
			}
			if pl.ID == 0 {
				return nil, errors.New("variable id is required")
			}

			if err := ctx.Snapshot("environment_variables", pl.ID); err != nil {
				return nil, err
			}

			result, err := ctx.Tx.Exec(
				`DELETE FROM environment_variables WHERE id = ?`, pl.ID,
			)
			if err != nil {
				return nil, fmt.Errorf("deleting variable: %w", err)
			}
			rows, err := result.RowsAffected()
			if err == nil && rows == 0 {
				return nil, errors.New("variable not found")
			}

			ctx.Affected("environment_variable", "delete", pl.ID)
			return nil, nil
		},
	}
}
