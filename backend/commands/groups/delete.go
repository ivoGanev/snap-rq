package groups

import (
	"database/sql"
	"encoding/json"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// delete.go — remove a group; its requests stay in the collection. Undo
// recreates the group under its old id and puts its requests back.

type Delete struct{ tx *sql.Tx }

func NewDelete(tx *sql.Tx) commands.Command { return &Delete{tx: tx} }

func (Delete) Info() commands.Info {
	return commands.Info{FunctionID: "group.delete", Label: "Ungroup"}
}

type deleteInput struct {
	ID int64 `json:"id"`
}

type deleteSnapshot struct {
	Group   models.RequestGroup `json:"group"`
	Members []int64             `json:"members"`
}

func (c Delete) Snapshot(input json.RawMessage) (any, error) {
	var in deleteInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	group, err := queryGroup(c.tx, in.ID)
	if err != nil {
		return nil, err
	}
	members, err := c.members(in.ID)
	if err != nil {
		return nil, err
	}
	return deleteSnapshot{Group: group, Members: members}, nil
}

func (c Delete) members(groupID int64) ([]int64, error) {
	rows, err := c.tx.Query(`SELECT id FROM http_requests WHERE group_id = ?`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (c Delete) Run(input json.RawMessage) (any, error) {
	var in deleteInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	members, err := c.members(in.ID)
	if err != nil {
		return nil, err
	}
	if _, err := c.tx.Exec(`DELETE FROM request_groups WHERE id = ?`, in.ID); err != nil {
		return nil, err
	}
	return map[string]any{"id": in.ID, "request_ids": members}, nil
}

func (c Delete) Undo(snapshot, _ json.RawMessage) error {
	var before deleteSnapshot
	if err := json.Unmarshal(snapshot, &before); err != nil {
		return err
	}
	g := before.Group
	if err := reinsertGroups(c.tx, []models.RequestGroup{g}); err != nil {
		return err
	}
	return assign(c.tx, &g.ID, before.Members)
}

func (Delete) Changes(_, result json.RawMessage) []commands.RowChange {
	return []commands.RowChange{
		{Component: "group", IDs: commands.IDsOf(result, "id")},
		{Component: "request", IDs: commands.IDsOf(result, "request_ids")},
	}
}
