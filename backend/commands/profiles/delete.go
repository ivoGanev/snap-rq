package profiles

import (
	"database/sql"
	"encoding/json"
	"errors"

	"snap-rq/backend/commands"
)

// delete.go — remove a profile and every project under it, permanently.
// Rare and deliberate; not undoable (see specs/command-framework.md).

type Delete struct{ tx *sql.Tx }

func NewDelete(tx *sql.Tx) commands.Command { return &Delete{tx: tx} }

func (Delete) Info() commands.Info {
	return commands.Info{FunctionID: "profile.delete", Label: "Delete Profile"}
}

func (c Delete) Run(input json.RawMessage) (any, error) {
	var in struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if in.ID == 0 {
		return nil, errors.New("profile id is required")
	}
	_, err := c.tx.Exec(`DELETE FROM profiles WHERE id = ?`, in.ID)
	return nil, err
}
