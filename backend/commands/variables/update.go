package variables

import (
	"database/sql"
	"encoding/json"

	"snap-rq/backend/commands"
)

// update.go — change a variable's key or value. Deliberately not
// undoable: edits are continuous typing, and variable values are often
// secrets — neither belongs on the undo stack. Create and delete stay
// undoable (they are discrete, and deleting one by accident is worth
// reversing).
//
// The value is optional: left out, it stays as it is. The editor leaves it
// out when renaming a secret, whose value it doesn't have. A secret's new
// value is encrypted. Whether a variable is secret changes only through
// variable.set_secret.

type Update struct{ tx *sql.Tx }

func NewUpdate(tx *sql.Tx) commands.Command { return &Update{tx: tx} }

func (Update) Info() commands.Info {
	return commands.Info{FunctionID: "variable.update", Label: "Update Variable"}
}

func (c Update) Run(input json.RawMessage) (any, error) {
	var in struct {
		ID    int64   `json:"id"`
		Key   string  `json:"key"`
		Value *string `json:"value"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	v, err := load(c.tx, in.ID)
	if err != nil {
		return nil, err
	}

	v.Key = in.Key
	if in.Value != nil {
		if v.Value, err = storedValue(c.tx, v.Secret, *in.Value); err != nil {
			return nil, err
		}
	}
	if _, err := c.tx.Exec(`UPDATE environment_variables SET key = ?, value = ? WHERE id = ?`,
		v.Key, v.Value, v.ID); err != nil {
		return nil, err
	}
	return forFrontend(v), nil
}
