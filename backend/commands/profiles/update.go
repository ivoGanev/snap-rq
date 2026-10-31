package profiles

import (
	"database/sql"
	"encoding/json"
	"errors"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// update.go — rename a profile. Snapshot saves the old name.

type Update struct{ tx *sql.Tx }

func NewUpdate(tx *sql.Tx) commands.Command { return &Update{tx: tx} }

func (Update) Info() commands.Info {
	return commands.Info{FunctionID: "profile.update", Label: "Rename Profile"}
}

func (c Update) Snapshot(input json.RawMessage) (any, error) {
	var in models.Profile
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	var oldName string
	if err := c.tx.QueryRow(
		`SELECT name FROM profiles WHERE id = ?`, in.ID).Scan(&oldName); err != nil {
		return nil, errors.New("profile not found")
	}
	return map[string]any{"id": in.ID, "name": oldName}, nil
}

func (c Update) Run(input json.RawMessage) (any, error) {
	var in models.Profile
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if in.ID == 0 {
		return nil, errors.New("profile id is required")
	}
	if _, err := c.tx.Exec(
		`UPDATE profiles SET name = ? WHERE id = ?`, in.Name, in.ID); err != nil {
		return nil, err
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
	_, err := c.tx.Exec(`UPDATE profiles SET name = ? WHERE id = ?`, old.Name, old.ID)
	return err
}
