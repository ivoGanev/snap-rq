package groups

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// arrange.go — save a collection's whole layout after a drag: the order of
// its top-level requests and groups, and each request's group and place in
// it. A group the drag leaves empty is deleted. Undo puts back every order,
// every membership and the deleted groups.

type Arrange struct{ tx *sql.Tx }

func NewArrange(tx *sql.Tx) commands.Command { return &Arrange{tx: tx} }

func (Arrange) Info() commands.Info {
	return commands.Info{FunctionID: "group.arrange", Label: "Move Requests"}
}

// arrangeItem is one row of the layout, in display order: a group, or a
// request with the group it sits in (0 = none).
type arrangeItem struct {
	Kind    string `json:"kind"`
	ID      int64  `json:"id"`
	GroupID int64  `json:"group_id"`
}

type arrangeInput struct {
	CollectionID int64         `json:"collection_id"`
	Items        []arrangeItem `json:"items"`
}

type requestPlace struct {
	ID        int64  `json:"id"`
	GroupID   *int64 `json:"group_id"`
	SortOrder int    `json:"user_sort_order"`
}

type arrangeSnapshot struct {
	Requests []requestPlace        `json:"requests"`
	Groups   []models.RequestGroup `json:"groups"`
}

type arrangeResult struct {
	RequestIDs      []int64               `json:"request_ids"`
	GroupIDs        []int64               `json:"group_ids"`
	RemovedGroups   []models.RequestGroup `json:"removed_groups"`
	RemovedGroupIDs []int64               `json:"removed_group_ids"`
}

func (c Arrange) read(collectionID int64) (arrangeSnapshot, error) {
	var snap arrangeSnapshot
	rows, err := c.tx.Query(`SELECT id, group_id, user_sort_order FROM http_requests WHERE collection_id = ?`, collectionID)
	if err != nil {
		return snap, fmt.Errorf("reading the layout: %w", err)
	}
	for rows.Next() {
		var p requestPlace
		if err := rows.Scan(&p.ID, &p.GroupID, &p.SortOrder); err != nil {
			rows.Close()
			return snap, err
		}
		snap.Requests = append(snap.Requests, p)
	}
	rows.Close()

	groupRows, err := c.tx.Query(`SELECT id, collection_id, name, color, sort_order FROM request_groups WHERE collection_id = ?`, collectionID)
	if err != nil {
		return snap, fmt.Errorf("reading the groups: %w", err)
	}
	defer groupRows.Close()
	for groupRows.Next() {
		var g models.RequestGroup
		if err := groupRows.Scan(&g.ID, &g.CollectionID, &g.Name, &g.Color, &g.SortOrder); err != nil {
			return snap, err
		}
		snap.Groups = append(snap.Groups, g)
	}
	return snap, groupRows.Err()
}

func (c Arrange) Snapshot(input json.RawMessage) (any, error) {
	var in arrangeInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	return c.read(in.CollectionID)
}

func (c Arrange) Run(input json.RawMessage) (any, error) {
	var in arrangeInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if in.CollectionID == 0 {
		return nil, errors.New("collection id is required")
	}
	before, err := c.read(in.CollectionID)
	if err != nil {
		return nil, err
	}

	result := arrangeResult{RequestIDs: []int64{}, GroupIDs: []int64{}}
	for index, item := range in.Items {
		switch item.Kind {
		case "group":
			if _, err := c.tx.Exec(`UPDATE request_groups SET sort_order = ? WHERE id = ? AND collection_id = ?`,
				index, item.ID, in.CollectionID); err != nil {
				return nil, err
			}
			result.GroupIDs = append(result.GroupIDs, item.ID)
		case "request":
			var group *int64
			if item.GroupID != 0 {
				group = &item.GroupID
			}
			if _, err := c.tx.Exec(`UPDATE http_requests SET user_sort_order = ?, group_id = ?
				WHERE id = ? AND collection_id = ?`, index, group, item.ID, in.CollectionID); err != nil {
				return nil, err
			}
			result.RequestIDs = append(result.RequestIDs, item.ID)
		default:
			return nil, fmt.Errorf("unknown item kind %q", item.Kind)
		}
	}

	var memberships []membership
	for _, p := range before.Requests {
		memberships = append(memberships, membership{ID: p.ID, GroupID: p.GroupID})
	}
	removed, err := dropEmptied(c.tx, memberships)
	if err != nil {
		return nil, err
	}
	result.RemovedGroups = removed
	result.RemovedGroupIDs = groupIDs(removed)
	return result, nil
}

func (c Arrange) Undo(snapshot, result json.RawMessage) error {
	var before arrangeSnapshot
	if err := json.Unmarshal(snapshot, &before); err != nil {
		return err
	}
	var done arrangeResult
	if err := json.Unmarshal(result, &done); err != nil {
		return err
	}
	if err := reinsertGroups(c.tx, done.RemovedGroups); err != nil {
		return err
	}
	for _, g := range before.Groups {
		if _, err := c.tx.Exec(`UPDATE request_groups SET sort_order = ? WHERE id = ?`, g.SortOrder, g.ID); err != nil {
			return err
		}
	}
	for _, p := range before.Requests {
		if _, err := c.tx.Exec(`UPDATE http_requests SET user_sort_order = ?, group_id = ? WHERE id = ?`,
			p.SortOrder, p.GroupID, p.ID); err != nil {
			return err
		}
	}
	return nil
}

func (Arrange) Changes(_, result json.RawMessage) []commands.RowChange {
	return []commands.RowChange{
		{Component: "request", IDs: commands.IDsOf(result, "request_ids")},
		{Component: "group", IDs: commands.IDsOf(result, "group_ids", "removed_group_ids")},
	}
}
