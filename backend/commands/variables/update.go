package variables

import (
	"database/sql"
	"encoding/json"
	"errors"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// update.go — change a variable's key or value. Deliberately not
// undoable: edits are continuous typing, and variable values are often
// secrets — neither belongs on the undo stack. Create and delete stay
// undoable (they are discrete, and deleting one by accident is worth
// reversing).

type Update struct{ tx *sql.Tx }

func NewUpdate(tx *sql.Tx) commands.Command { return &Update{tx: tx} }

func (Update) Info() commands.Info {
	return commands.Info{FunctionID: "variable.update", Label: "Update Variable"}
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
