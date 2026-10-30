package services

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"snap-rq/backend/commands"
)

// EnvironmentDeleteCommandName is the public command name for deleting an
// environment.
const EnvironmentDeleteCommandName = "environment.delete"

// RegisterCommands makes this service's commands runnable and routes its
// mutations through the history.
func (s *EnvironmentService) RegisterCommands(history *commands.History) error {
	s.history = history
	return history.Register(s.deleteCommand())
}

// deleteCommand removes an environment together with its variables; the
// default environment is refused. Undo puts all of it back as it was.
func (s *EnvironmentService) deleteCommand() *commands.Command {
	return &commands.Command{
		Name:     EnvironmentDeleteCommandName,
		Label:    "Delete Environment",
		Undoable: true,
		Apply: func(tx *sql.Tx, rec *commands.Recorder, input json.RawMessage) (any, error) {
			var payload struct {
				ID int64 `json:"id"`
			}
			if err := json.Unmarshal(input, &payload); err != nil {
				return nil, err
			}
			if payload.ID == 0 {
				return nil, errors.New("environment id is required")
			}

			var name string
			if err := tx.QueryRow(
				`SELECT name FROM environments WHERE id = ?`, payload.ID,
			).Scan(&name); err != nil {
				if err == sql.ErrNoRows {
					return nil, errors.New("environment not found")
				}
				return nil, fmt.Errorf("loading environment: %w", err)
			}
			if strings.EqualFold(name, "Default") {
				return nil, errors.New("cannot delete the default environment")
			}

			variableIDs, err := selectIDsTx(tx,
				`SELECT id FROM environment_variables WHERE environment_id = ?`, payload.ID)
			if err != nil {
				return nil, fmt.Errorf("listing environment variables: %w", err)
			}

			// Parent table first: rows are put back in save order.
			if err := rec.SaveRows("environments", payload.ID); err != nil {
				return nil, err
			}
			if err := rec.SaveRows("environment_variables", variableIDs...); err != nil {
				return nil, err
			}

			if _, err := tx.Exec(
				`DELETE FROM environments WHERE id = ?`, payload.ID,
			); err != nil {
				return nil, fmt.Errorf("deleting environment: %w", err)
			}

			rec.Touched("environment", "delete", payload.ID)
			return nil, nil
		},
	}
}
