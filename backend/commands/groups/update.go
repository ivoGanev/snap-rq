package groups

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// update.go — rename a group or change its colour. Undo writes the old row
// back.

type Update struct{ tx *sql.Tx }

func NewUpdate(tx *sql.Tx) commands.Command { return &Update{tx: tx} }

func (Update) Info() commands.Info {
	return commands.Info{FunctionID: "group.update", Label: "Edit Group"}
}

func (c Update) Snapshot(input json.RawMessage) (any, error) {
	var in models.RequestGroup
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	return queryGroup(c.tx, in.ID)
}

func (c Update) Run(input json.RawMessage) (any, error) {
	var in models.RequestGroup
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if in.ID == 0 {
		return nil, errors.New("group id is required")
	}
	if in.Color != nil && !models.IsColorSlot(*in.Color) {
		return nil, fmt.Errorf("unknown colour %q", *in.Color)
	}
	if _, err := c.tx.Exec(`UPDATE request_groups SET name = ?, color = ? WHERE id = ?`,
		in.Name, in.Color, in.ID); err != nil {
		return nil, err
	}
	return queryGroup(c.tx, in.ID)
}

func (c Update) Undo(snapshot, _ json.RawMessage) error {
	var before models.RequestGroup
	if err := json.Unmarshal(snapshot, &before); err != nil {
		return err
	}
	_, err := c.tx.Exec(`UPDATE request_groups SET name = ?, color = ? WHERE id = ?`,
		before.Name, before.Color, before.ID)
	return err
}

func (Update) Changes(input, _ json.RawMessage) []commands.RowChange {
	return []commands.RowChange{{Component: "group", IDs: commands.IDsOf(input, "id")}}
}
