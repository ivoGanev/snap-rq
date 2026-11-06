// Package profiles holds the profile commands. Profiles only group
// projects; deleting one is a permanent action (rare and deliberate, no
// undo) matching the spec's classification.
package profiles

import (
	"database/sql"
	"encoding/json"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// create.go — save a new profile. Undo removes it; cascades take its
// projects with it.

type Create struct{ tx *sql.Tx }

func NewCreate(tx *sql.Tx) commands.Command { return &Create{tx: tx} }

func (Create) Info() commands.Info {
	return commands.Info{FunctionID: "profile.create", Label: "Create Profile"}
}

func (Create) Snapshot(_ json.RawMessage) (any, error) { return nil, nil }

func (c Create) Run(input json.RawMessage) (any, error) {
	var in models.Profile
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	result, err := c.tx.Exec(`INSERT INTO profiles (name) VALUES (?)`, in.Name)
	if err != nil {
		return nil, err
	}
	id, _ := result.LastInsertId()
	in.ID = id
	return in, nil
}

func (c Create) Undo(_, result json.RawMessage) error {
	var created models.Profile
	if err := json.Unmarshal(result, &created); err != nil {
		return err
	}
	_, err := c.tx.Exec(`DELETE FROM profiles WHERE id = ?`, created.ID)
	return err
}

func (Create) Changes(_, result json.RawMessage) []commands.RowChange {
	return []commands.RowChange{{Component: "profile", IDs: commands.IDsOf(result, "id")}}
}
