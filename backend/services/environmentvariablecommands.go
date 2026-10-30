package services

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"snap-rq/backend/commands"
)

// EnvironmentVariableDeleteCommandName is the public command name for
// deleting a single environment variable.
const EnvironmentVariableDeleteCommandName = "environment_variable.delete"

// RegisterCommands makes this service's commands runnable and routes its
// mutations through the history.
func (s *EnvironmentVariableService) RegisterCommands(history *commands.History) error {
	s.history = history
	return history.Register(s.deleteCommand())
}

// deleteCommand removes one variable. Undo puts the exact key/value row
// back as it was.
func (s *EnvironmentVariableService) deleteCommand() *commands.Command {
	return &commands.Command{
		Name:     EnvironmentVariableDeleteCommandName,
		Label:    "Delete Variable",
		Undoable: true,
		Apply: func(tx *sql.Tx, rec *commands.Recorder, input json.RawMessage) (any, error) {
			var payload struct {
				ID int64 `json:"id"`
			}
			if err := json.Unmarshal(input, &payload); err != nil {
				return nil, err
			}
			if payload.ID == 0 {
				return nil, errors.New("variable id is required")
			}

			if err := rec.SaveRows("environment_variables", payload.ID); err != nil {
				return nil, err
			}

			result, err := tx.Exec(
				`DELETE FROM environment_variables WHERE id = ?`, payload.ID,
			)
			if err != nil {
				return nil, fmt.Errorf("deleting variable: %w", err)
			}
			rows, err := result.RowsAffected()
			if err == nil && rows == 0 {
				return nil, errors.New("variable not found")
			}

			rec.Touched("environment_variable", "delete", payload.ID)
			return nil, nil
		},
	}
}
