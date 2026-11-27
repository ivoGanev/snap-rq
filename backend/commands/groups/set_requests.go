package groups

import (
	"database/sql"
	"encoding/json"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// set_requests.go — move requests into a group, or out of any group when
// group_id is 0. A group left with no requests is deleted. Undo brings back
// the deleted groups and puts each request back in the group it had.

type SetRequests struct{ tx *sql.Tx }

func NewSetRequests(tx *sql.Tx) commands.Command { return &SetRequests{tx: tx} }

func (SetRequests) Info() commands.Info {
	return commands.Info{FunctionID: "group.set_requests", Label: "Move to Group"}
}

type setRequestsInput struct {
	GroupID    int64   `json:"group_id"`
	RequestIDs []int64 `json:"request_ids"`
}

type setRequestsResult struct {
	RequestIDs      []int64               `json:"request_ids"`
	RemovedGroups   []models.RequestGroup `json:"removed_groups"`
	RemovedGroupIDs []int64               `json:"removed_group_ids"`
}

func (c SetRequests) Snapshot(input json.RawMessage) (any, error) {
	var in setRequestsInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	return readMemberships(c.tx, in.RequestIDs)
}

func (c SetRequests) Run(input json.RawMessage) (any, error) {
	var in setRequestsInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	before, err := readMemberships(c.tx, in.RequestIDs)
	if err != nil {
		return nil, err
	}
	var group *int64
	if in.GroupID != 0 {
		group = &in.GroupID
	}
	if err := assign(c.tx, group, in.RequestIDs); err != nil {
		return nil, err
	}
	removed, err := dropEmptied(c.tx, before)
	if err != nil {
		return nil, err
	}
	return setRequestsResult{RequestIDs: in.RequestIDs, RemovedGroups: removed, RemovedGroupIDs: groupIDs(removed)}, nil
}

func (c SetRequests) Undo(snapshot, result json.RawMessage) error {
	var before []membership
	if err := json.Unmarshal(snapshot, &before); err != nil {
		return err
	}
	var done setRequestsResult
	if err := json.Unmarshal(result, &done); err != nil {
		return err
	}
	if err := reinsertGroups(c.tx, done.RemovedGroups); err != nil {
		return err
	}
	return restoreMemberships(c.tx, before)
}

func (SetRequests) Changes(input, result json.RawMessage) []commands.RowChange {
	return []commands.RowChange{
		{Component: "request", IDs: commands.IDsOf(input, "request_ids")},
		{Component: "group", IDs: commands.IDsOf(result, "removed_group_ids")},
	}
}
