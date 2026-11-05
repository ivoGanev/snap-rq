package projects

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// update.go — rename a project. Snapshot saves the old name.

type Update struct{ tx *sql.Tx }

func NewUpdate(tx *sql.Tx) commands.Command { return &Update{tx: tx} }

func (Update) Info() commands.Info {
	return commands.Info{FunctionID: "project.update", Label: "Rename Project"}
}

func (c Update) Snapshot(input json.RawMessage) (any, error) {
	var in models.Project
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	var oldName string
	if err := c.tx.QueryRow(
		`SELECT name FROM projects WHERE id = ?`, in.ID,
	).Scan(&oldName); err != nil {
		return nil, fmt.Errorf("loading project: %w", err)
	}
	return map[string]any{"id": in.ID, "name": oldName}, nil
}

func (c Update) Run(input json.RawMessage) (any, error) {
	var in models.Project
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if in.ID == 0 {
		return nil, fmt.Errorf("project id is required")
	}
	if _, err := c.tx.Exec(
		`UPDATE projects SET profile_id = ?, name = ? WHERE id = ?`,
		in.ProfileID, in.Name, in.ID,
	); err != nil {
		return nil, fmt.Errorf("updating project: %w", err)
	}
	return in, nil
}

func (c Update) Undo(snapshot, _ json.RawMessage) error {
	var old struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(snapshot, &old); err != nil {
		return err
	}
	_, err := c.tx.Exec(`UPDATE projects SET name = ? WHERE id = ?`, old.Name, old.ID)
	return err
}

func (Update) Affected(input, _ json.RawMessage) []commands.Affected {
	return []commands.Affected{{Component: "project", IDs: commands.IDsOf(input, "id")}}
}
