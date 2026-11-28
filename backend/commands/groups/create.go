package groups

import (
	"database/sql"
	"encoding/json"
	"errors"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// create.go — a new group around some requests of one collection. A group
// the requests leave empty is deleted. Undo removes the new group, brings
// back the deleted ones and puts the requests back in their old groups.

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

type createResult struct {
	models.RequestGroup
	RemovedGroups   []models.RequestGroup `json:"removed_groups"`
	RemovedGroupIDs []int64               `json:"removed_group_ids"`
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
	if len(in.RequestIDs) == 0 {
		return nil, errors.New("a group needs at least one request")
	}
	before, err := readMemberships(c.tx, in.RequestIDs)
	if err != nil {
		return nil, err
	}
	// The group takes the place of the first of its requests.
	var order int
	if err := c.tx.QueryRow(
		`SELECT COALESCE(MIN(user_sort_order), 0) FROM http_requests WHERE id IN (`+placeholders(len(in.RequestIDs))+`)`,
		int64Args(in.RequestIDs)...,
	).Scan(&order); err != nil {
		return nil, err
	}
	result, err := c.tx.Exec(`INSERT INTO request_groups (collection_id, name, sort_order) VALUES (?, ?, ?)`,
		in.CollectionID, in.Name, order)
	if err != nil {
		return nil, err
	}
	id, _ := result.LastInsertId()
	if err := assign(c.tx, &id, in.RequestIDs); err != nil {
		return nil, err
	}
	removed, err := dropEmptied(c.tx, before)
	if err != nil {
		return nil, err
	}
	return createResult{
		RequestGroup:    models.RequestGroup{ID: id, CollectionID: in.CollectionID, Name: in.Name, SortOrder: order},
		RemovedGroups:   removed,
		RemovedGroupIDs: groupIDs(removed),
	}, nil
}

func (c Create) Undo(snapshot, result json.RawMessage) error {
	var before []membership
	if err := json.Unmarshal(snapshot, &before); err != nil {
		return err
	}
	var created createResult
	if err := json.Unmarshal(result, &created); err != nil {
		return err
	}
	if _, err := c.tx.Exec(`DELETE FROM request_groups WHERE id = ?`, created.ID); err != nil {
		return err
	}
	if err := reinsertGroups(c.tx, created.RemovedGroups); err != nil {
		return err
	}
	return restoreMemberships(c.tx, before)
}

func (Create) Changes(input, result json.RawMessage) []commands.RowChange {
	return []commands.RowChange{
		{Component: "group", IDs: commands.IDsOf(result, "id", "removed_group_ids")},
		{Component: "request", IDs: commands.IDsOf(input, "request_ids")},
	}
}
