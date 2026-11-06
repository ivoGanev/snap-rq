package projects

import (
	"database/sql"
	"encoding/json"
	"errors"

	"snap-rq/backend/commands"
)

// delete.go — remove a project with its entire tree (collections, requests,
// responses, environments, favourites, tags). Deliberately not undoable:
// the tree can be massive, and the user confirmed they want it gone.

type Delete struct{ tx *sql.Tx }

func NewDelete(tx *sql.Tx) commands.Command { return &Delete{tx: tx} }

func (Delete) Info() commands.Info {
	return commands.Info{FunctionID: "project.delete", Label: "Delete Project"}
}

func (c Delete) Run(input json.RawMessage) (any, error) {
	var in struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if in.ID == 0 {
		return nil, errors.New("project id is required")
	}
	_, err := c.tx.Exec(`DELETE FROM projects WHERE id = ?`, in.ID)
	return nil, err
}
