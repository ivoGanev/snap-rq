package variables

import (
	"database/sql"
	"encoding/json"
	"errors"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// delete.go — remove one variable. Snapshot saves the exact key/value row;
// undo re-inserts it under its original id.

type Delete struct{ tx *sql.Tx }

func NewDelete(tx *sql.Tx) commands.Command { return &Delete{tx: tx} }

func (Delete) Info() commands.Info {
	return commands.Info{FunctionID: "variable.delete", Label: "Delete Variable"}
}

func (c Delete) Snapshot(input json.RawMessage) (any, error) {
	var in struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	var saved models.EnvironmentVariable
	err := c.tx.QueryRow(
		`SELECT id, environment_id, key, value FROM environment_variables WHERE id = ?`, in.ID,
	).Scan(&saved.ID, &saved.EnvironmentID, &saved.Key, &saved.Value)
	if err != nil {
		return nil, errors.New("variable not found")
	}
	return saved, nil
}

func (c Delete) Run(input json.RawMessage) (any, error) {
	var in struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if in.ID == 0 {
		return nil, errors.New("variable id is required")
	}
	result, err := c.tx.Exec(`DELETE FROM environment_variables WHERE id = ?`, in.ID)
	if err != nil {
		return nil, err
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return nil, errors.New("variable not found")
	}
	return nil, nil
}

func (c Delete) Undo(snapshot, _ json.RawMessage) error {
	var saved models.EnvironmentVariable
	if err := json.Unmarshal(snapshot, &saved); err != nil {
		return err
	}
	_, err := c.tx.Exec(
		`INSERT INTO environment_variables (id, environment_id, key, value) VALUES (?, ?, ?, ?)`,
		saved.ID, saved.EnvironmentID, saved.Key, saved.Value)
	return err
}

func (Delete) Changes(input, _ json.RawMessage) []commands.RowChange {
	return []commands.RowChange{{Component: "variable", IDs: commands.IDsOf(input, "id")}}
}
