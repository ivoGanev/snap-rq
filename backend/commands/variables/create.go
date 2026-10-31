// Package variables holds the environment variable commands, one
// operation per file.
package variables

import (
	"database/sql"
	"encoding/json"
	"errors"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// create.go — add one variable to an environment. Undo removes it.

type Create struct{ tx *sql.Tx }

func NewCreate(tx *sql.Tx) commands.Command { return &Create{tx: tx} }

func (Create) Info() commands.Info {
	return commands.Info{FunctionID: "variable.create", Label: "Create Variable"}
}

func (Create) Snapshot(_ json.RawMessage) (any, error) { return nil, nil }

func (c Create) Run(input json.RawMessage) (any, error) {
	var in struct {
		EnvironmentID int64  `json:"environment_id"`
		Key           string `json:"key"`
		Value         string `json:"value"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if in.EnvironmentID == 0 {
		return nil, errors.New("environment id is required")
	}

	result, err := c.tx.Exec(
		`INSERT INTO environment_variables (environment_id, key, value) VALUES (?, ?, ?)`,
		in.EnvironmentID, in.Key, in.Value)
	if err != nil {
		return nil, err
	}
	id, _ := result.LastInsertId()
	return models.EnvironmentVariable{
		ID: id, EnvironmentID: in.EnvironmentID, Key: in.Key, Value: in.Value,
	}, nil
}

func (c Create) Undo(_, result json.RawMessage) error {
	var created models.EnvironmentVariable
	if err := json.Unmarshal(result, &created); err != nil {
		return err
	}
	_, err := c.tx.Exec(`DELETE FROM environment_variables WHERE id = ?`, created.ID)
	return err
}
