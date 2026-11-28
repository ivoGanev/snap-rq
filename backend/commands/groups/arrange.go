package groups

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// arrange.go — save a collection's layout after a drag: the order of its
// top-level requests and groups, and each request's group and place in it.
// Only rows whose place changed are written and reported. A group the drag
// leaves empty is deleted. Undo puts back the changed rows and the deleted
// groups.

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

func sameGroup(a, b *int64) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
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
	requestsBefore := make(map[int64]requestPlace, len(before.Requests))
	for _, p := range before.Requests {
		requestsBefore[p.ID] = p
	}
	groupsBefore := make(map[int64]models.RequestGroup, len(before.Groups))
	for _, g := range before.Groups {
		groupsBefore[g.ID] = g
	}

	// Items outside this collection are ignored, as are items already in place.
	var moved []requestPlace
	var left []membership
	var movedGroups []models.RequestGroup
	for index, item := range in.Items {
		switch item.Kind {
		case "group":
			g, ok := groupsBefore[item.ID]
			if !ok || g.SortOrder == index {
				continue
			}
			g.SortOrder = index
			movedGroups = append(movedGroups, g)
		case "request":
			old, ok := requestsBefore[item.ID]
			if !ok {
				continue
			}
			var group *int64
			if item.GroupID != 0 {
				group = &item.GroupID
			}
			if old.SortOrder == index && sameGroup(old.GroupID, group) {
				continue
			}
			moved = append(moved, requestPlace{ID: item.ID, GroupID: group, SortOrder: index})
			if !sameGroup(old.GroupID, group) {
				left = append(left, membership{ID: old.ID, GroupID: old.GroupID})
			}
		default:
			return nil, fmt.Errorf("unknown item kind %q", item.Kind)
		}
	}

	if err := setPlaces(c.tx, moved); err != nil {
		return nil, err
	}
	if err := setGroupOrders(c.tx, movedGroups); err != nil {
		return nil, err
	}
	removed, err := dropEmptied(c.tx, left)
	if err != nil {
		return nil, err
	}

	result := arrangeResult{
		RequestIDs:      make([]int64, len(moved)),
		GroupIDs:        groupIDs(movedGroups),
		RemovedGroups:   removed,
		RemovedGroupIDs: groupIDs(removed),
	}
	for i, p := range moved {
		result.RequestIDs[i] = p.ID
	}
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

	movedGroups := map[int64]bool{}
	for _, id := range done.GroupIDs {
		movedGroups[id] = true
	}
	var groups []models.RequestGroup
	for _, g := range before.Groups {
		if movedGroups[g.ID] {
			groups = append(groups, g)
		}
	}
	if err := setGroupOrders(c.tx, groups); err != nil {
		return err
	}

	moved := map[int64]bool{}
	for _, id := range done.RequestIDs {
		moved[id] = true
	}
	var places []requestPlace
	for _, p := range before.Requests {
		if moved[p.ID] {
			places = append(places, p)
		}
	}
	return setPlaces(c.tx, places)
}

func (Arrange) Changes(_, result json.RawMessage) []commands.RowChange {
	return []commands.RowChange{
		{Component: "request", IDs: commands.IDsOf(result, "request_ids")},
		{Component: "group", IDs: commands.IDsOf(result, "group_ids", "removed_group_ids")},
	}
}
