// Package environments holds the environment commands, one operation per
// file.
package environments

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// create.go — save a new environment. Undo removes it; cascades take its
// variables with it.

type Create struct{ tx *sql.Tx }

func NewCreate(tx *sql.Tx) commands.Command { return &Create{tx: tx} }

func (Create) Info() commands.Info {
	return commands.Info{FunctionID: "environment.create", Label: "Create Environment"}
}

func (Create) Snapshot(_ json.RawMessage) (any, error) { return nil, nil }

func (c Create) Run(input json.RawMessage) (any, error) {
	var in struct {
		ProjectID int64  `json:"project_id"`
		Name      string `json:"name"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if in.ProjectID == 0 {
		return nil, errors.New("project id is required")
	}

	created := time.Now().UTC()
	result, err := c.tx.Exec(
		`INSERT INTO environments (project_id, name, created_at) VALUES (?, ?, ?)`,
		in.ProjectID, in.Name, created.Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		return nil, err
	}
	id, _ := result.LastInsertId()
	return models.Environment{ID: id, ProjectID: in.ProjectID, Name: in.Name, CreatedAt: created}, nil
}

func (c Create) Undo(_, result json.RawMessage) error {
	var created models.Environment
	if err := json.Unmarshal(result, &created); err != nil {
		return err
	}
	_, err := c.tx.Exec(`DELETE FROM environments WHERE id = ?`, created.ID)
	return err
}

func (Create) Changes(_, result json.RawMessage) []commands.RowChange {
	return []commands.RowChange{{Component: "environment", IDs: commands.IDsOf(result, "id")}}
}
