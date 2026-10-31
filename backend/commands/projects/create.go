// Package projects holds the project commands. The delete snapshot saves
// the entire project tree (collections, requests, responses, tag and
// favourite links, environments, variables, favourite collections,
// appearances, tags) — undo re-inserts all of it under the original ids.
package projects

import (
	"database/sql"
	"encoding/json"
	"errors"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// create.go — save a new project. Undo removes it; cascades take
// everything created under it.

type Create struct{ tx *sql.Tx }

func NewCreate(tx *sql.Tx) commands.Command { return &Create{tx: tx} }

func (Create) Info() commands.Info {
	return commands.Info{FunctionID: "project.create", Label: "Create Project"}
}

func (Create) Snapshot(_ json.RawMessage) (any, error) { return nil, nil }

func (c Create) Run(input json.RawMessage) (any, error) {
	var in models.Project
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if in.ProfileID == 0 {
		return nil, errors.New("profile id is required")
	}
	result, err := c.tx.Exec(
		`INSERT INTO projects (profile_id, name) VALUES (?, ?)`, in.ProfileID, in.Name)
	if err != nil {
		return nil, err
	}
	id, _ := result.LastInsertId()
	in.ID = id
	return in, nil
}

func (c Create) Undo(_, result json.RawMessage) error {
	var created models.Project
	if err := json.Unmarshal(result, &created); err != nil {
		return err
	}
	_, err := c.tx.Exec(`DELETE FROM projects WHERE id = ?`, created.ID)
	return err
}
