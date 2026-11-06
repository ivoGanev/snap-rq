package variables

import (
	"database/sql"
	"encoding/json"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// list.go — an environment's variables, ordered by key.

type List struct{ tx *sql.Tx }

func NewList(tx *sql.Tx) commands.Command { return &List{tx: tx} }

func (List) Info() commands.Info {
	return commands.Info{FunctionID: "variable.list", Label: "List Variables", ReadOnly: true}
}

func (c List) Run(input json.RawMessage) (any, error) {
	var in struct {
		EnvironmentID int64 `json:"environment_id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	rows, err := c.tx.Query(
		`SELECT id, environment_id, key, value FROM environment_variables
		 WHERE environment_id = ? ORDER BY key`, in.EnvironmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var variables []models.EnvironmentVariable
	for rows.Next() {
		var v models.EnvironmentVariable
		if err := rows.Scan(&v.ID, &v.EnvironmentID, &v.Key, &v.Value); err != nil {
			return nil, err
		}
		variables = append(variables, v)
	}
	return variables, rows.Err()
}
