package environments

import (
	"database/sql"
	"encoding/json"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// list.go — a project's environments, newest first.

type List struct{ tx *sql.Tx }

func NewList(tx *sql.Tx) commands.Command { return &List{tx: tx} }

func (List) Info() commands.Info {
	return commands.Info{FunctionID: "environment.list", Label: "List Environments", ReadOnly: true}
}

func (c List) Run(input json.RawMessage) (any, error) {
	var in struct {
		ProjectID int64 `json:"project_id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	rows, err := c.tx.Query(
		`SELECT id, project_id, name, created_at FROM environments
		 WHERE project_id = ? ORDER BY created_at DESC`, in.ProjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var environments []models.Environment
	for rows.Next() {
		var env models.Environment
		if err := rows.Scan(&env.ID, &env.ProjectID, &env.Name, &env.CreatedAt); err != nil {
			return nil, err
		}
		environments = append(environments, env)
	}
	return environments, rows.Err()
}
