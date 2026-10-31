package variables

import (
	"database/sql"
	"encoding/json"
	"errors"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// update.go — change a variable's key or value. Snapshot saves the old
// key/value pair.

type Update struct{ tx *sql.Tx }

func NewUpdate(tx *sql.Tx) commands.Command { return &Update{tx: tx} }

func (Update) Info() commands.Info {
	return commands.Info{FunctionID: "variable.update", Label: "Update Variable"}
}

func (c Update) Snapshot(input json.RawMessage) (any, error) {
	var in models.EnvironmentVariable
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	var old models.EnvironmentVariable
	err := c.tx.QueryRow(
		`SELECT id, environment_id, key, value FROM environment_variables WHERE id = ?`, in.ID,
	).Scan(&old.ID, &old.EnvironmentID, &old.Key, &old.Value)
	if err != nil {
		return nil, errors.New("variable not found")
	}
	return old, nil
}

func (c Update) Run(input json.RawMessage) (any, error) {
	var in models.EnvironmentVariable
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if in.ID == 0 {
		return nil, errors.New("variable id is required")
	}
	if _, err := c.tx.Exec(
		`UPDATE environment_variables SET environment_id = ?, key = ?, value = ? WHERE id = ?`,
		in.EnvironmentID, in.Key, in.Value, in.ID,
	); err != nil {
		return nil, err
	}
	return in, nil
}

func (c Update) Undo(snapshot, _ json.RawMessage) error {
	var old models.EnvironmentVariable
	if err := json.Unmarshal(snapshot, &old); err != nil {
		return err
	}
	_, err := c.tx.Exec(
		`UPDATE environment_variables SET key = ?, value = ? WHERE id = ?`,
		old.Key, old.Value, old.ID)
	return err
}
