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
// environment (see specs/command-framework.md).
const EnvironmentDeleteCommandName = "environment.delete"

// RegisterCommands registers this service's commands on the bus and routes
// this service's mutations through it.
func (s *EnvironmentService) RegisterCommands(bus *commands.Bus) error {
	s.bus = bus
	return bus.Register(s.deleteCommand())
}

// deleteCommand removes an environment and its variables as one undoable
// entry; the default environment is refused. Undo restores the environment
// and every variable row under their original ids.
func (s *EnvironmentService) deleteCommand() *commands.Command {
	return &commands.Command{
		Name:     EnvironmentDeleteCommandName,
		Label:    "Delete Environment",
		Category: commands.Undoable,
		Handler: func(ctx *commands.Context, payload json.RawMessage) (any, error) {
			var pl struct {
				ID int64 `json:"id"`
			}
			if err := json.Unmarshal(payload, &pl); err != nil {
				return nil, err
			}
			if pl.ID == 0 {
				return nil, errors.New("environment id is required")
			}

			var name string
			if err := ctx.Tx.QueryRow(
				`SELECT name FROM environments WHERE id = ?`, pl.ID,
			).Scan(&name); err != nil {
				if err == sql.ErrNoRows {
					return nil, errors.New("environment not found")
				}
				return nil, fmt.Errorf("loading environment: %w", err)
			}
			if strings.EqualFold(name, "Default") {
				return nil, errors.New("cannot delete the default environment")
			}

			variableIDs, err := selectIDsTx(ctx.Tx,
				`SELECT id FROM environment_variables WHERE environment_id = ?`, pl.ID)
			if err != nil {
				return nil, fmt.Errorf("listing environment variables: %w", err)
			}

			// Parent table first: capture order becomes restore order.
			if err := ctx.Snapshot("environments", pl.ID); err != nil {
				return nil, err
			}
			if err := ctx.Snapshot("environment_variables", variableIDs...); err != nil {
				return nil, err
			}

			if _, err := ctx.Tx.Exec(
				`DELETE FROM environments WHERE id = ?`, pl.ID,
			); err != nil {
				return nil, fmt.Errorf("deleting environment: %w", err)
			}

			ctx.Affected("environment", "delete", pl.ID)
			return nil, nil
		},
	}
}
