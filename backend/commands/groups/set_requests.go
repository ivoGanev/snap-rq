package groups

import (
	"database/sql"
	"encoding/json"

	"snap-rq/backend/commands"
)

// set_requests.go — move requests into a group, or out of any group when
// group_id is 0. Undo puts each back in the group it had.

type SetRequests struct{ tx *sql.Tx }

func NewSetRequests(tx *sql.Tx) commands.Command { return &SetRequests{tx: tx} }

func (SetRequests) Info() commands.Info {
	return commands.Info{FunctionID: "group.set_requests", Label: "Move to Group"}
}

type setRequestsInput struct {
	GroupID    int64   `json:"group_id"`
	RequestIDs []int64 `json:"request_ids"`
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
	var group *int64
	if in.GroupID != 0 {
		group = &in.GroupID
	}
	if err := assign(c.tx, group, in.RequestIDs); err != nil {
		return nil, err
	}
	return map[string]any{"request_ids": in.RequestIDs}, nil
}

func (c SetRequests) Undo(snapshot, _ json.RawMessage) error {
	var before []membership
	if err := json.Unmarshal(snapshot, &before); err != nil {
		return err
	}
	return restoreMemberships(c.tx, before)
}

func (SetRequests) Changes(input, _ json.RawMessage) []commands.RowChange {
	return []commands.RowChange{{Component: "request", IDs: commands.IDsOf(input, "request_ids")}}
}
