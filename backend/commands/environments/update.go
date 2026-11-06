package environments

import (
	"database/sql"
	"encoding/json"
	"errors"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// update.go — rename an environment. Snapshot saves the old name.

type Update struct{ tx *sql.Tx }

func NewUpdate(tx *sql.Tx) commands.Command { return &Update{tx: tx} }

func (Update) Info() commands.Info {
	return commands.Info{FunctionID: "environment.update", Label: "Rename Environment"}
}

func (c Update) Snapshot(input json.RawMessage) (any, error) {
	var env models.Environment
	if err := json.Unmarshal(input, &env); err != nil {
		return nil, err
	}
	var oldName string
	if err := c.tx.QueryRow(
		`SELECT name FROM environments WHERE id = ?`, env.ID,
	).Scan(&oldName); err != nil {
		return nil, errors.New("environment not found")
	}
	return map[string]any{"id": env.ID, "name": oldName}, nil
}

func (c Update) Run(input json.RawMessage) (any, error) {
	var env models.Environment
	if err := json.Unmarshal(input, &env); err != nil {
		return nil, err
	}
	if env.ID == 0 {
		return nil, errors.New("environment id is required")
	}
	if _, err := c.tx.Exec(
		`UPDATE environments SET project_id = ?, name = ?, created_at = ? WHERE id = ?`,
		env.ProjectID, env.Name, env.CreatedAt, env.ID,
	); err != nil {
		return nil, err
	}
	return env, nil
}

func (c Update) Undo(snapshot, _ json.RawMessage) error {
	var old struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(snapshot, &old); err != nil {
		return err
	}
	_, err := c.tx.Exec(`UPDATE environments SET name = ? WHERE id = ?`, old.Name, old.ID)
	return err
}

func (Update) Changes(input, _ json.RawMessage) []commands.RowChange {
	return []commands.RowChange{{Component: "environment", IDs: commands.IDsOf(input, "id")}}
}
