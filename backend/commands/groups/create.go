package groups

import (
	"database/sql"
	"encoding/json"
	"errors"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// create.go — a new group around some requests of one collection. Undo puts
// the requests back in their old groups and removes the new one.

type Create struct{ tx *sql.Tx }

func NewCreate(tx *sql.Tx) commands.Command { return &Create{tx: tx} }

func (Create) Info() commands.Info {
	return commands.Info{FunctionID: "group.create", Label: "Group Requests"}
}

type createInput struct {
	CollectionID int64   `json:"collection_id"`
	Name         string  `json:"name"`
	RequestIDs   []int64 `json:"request_ids"`
}

func (c Create) Snapshot(input json.RawMessage) (any, error) {
	var in createInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	return readMemberships(c.tx, in.RequestIDs)
}

func (c Create) Run(input json.RawMessage) (any, error) {
	var in createInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if in.CollectionID == 0 {
		return nil, errors.New("collection id is required")
	}
	result, err := c.tx.Exec(`INSERT INTO request_groups (collection_id, name) VALUES (?, ?)`,
		in.CollectionID, in.Name)
	if err != nil {
		return nil, err
	}
	id, _ := result.LastInsertId()
	if err := assign(c.tx, &id, in.RequestIDs); err != nil {
		return nil, err
	}
	return models.RequestGroup{ID: id, CollectionID: in.CollectionID, Name: in.Name}, nil
}

func (c Create) Undo(snapshot, result json.RawMessage) error {
	var before []membership
	if err := json.Unmarshal(snapshot, &before); err != nil {
		return err
	}
	var created models.RequestGroup
	if err := json.Unmarshal(result, &created); err != nil {
		return err
	}
	if _, err := c.tx.Exec(`DELETE FROM request_groups WHERE id = ?`, created.ID); err != nil {
		return err
	}
	return restoreMemberships(c.tx, before)
}

func (Create) Changes(input, result json.RawMessage) []commands.RowChange {
	return []commands.RowChange{
		{Component: "group", IDs: commands.IDsOf(result, "id")},
		{Component: "request", IDs: commands.IDsOf(input, "request_ids")},
	}
}
