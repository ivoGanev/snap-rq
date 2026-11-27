package groups

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"snap-rq/backend/commands"
)

// reorder.go — set the order of a collection's groups. Undo writes each
// group's old position back.

type Reorder struct{ tx *sql.Tx }

func NewReorder(tx *sql.Tx) commands.Command { return &Reorder{tx: tx} }

func (Reorder) Info() commands.Info {
	return commands.Info{FunctionID: "group.reorder", Label: "Reorder Groups"}
}

type reorderInput struct {
	CollectionID int64   `json:"collection_id"`
	OrderedIDs   []int64 `json:"ordered_ids"`
}

type position struct {
	ID        int64 `json:"id"`
	SortOrder int   `json:"sort_order"`
}

func (c Reorder) Snapshot(input json.RawMessage) (any, error) {
	var in reorderInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	rows, err := c.tx.Query(`SELECT id, sort_order FROM request_groups WHERE collection_id = ?`, in.CollectionID)
	if err != nil {
		return nil, fmt.Errorf("loading the group order: %w", err)
	}
	defer rows.Close()
	saved := []position{}
	for rows.Next() {
		var p position
		if err := rows.Scan(&p.ID, &p.SortOrder); err != nil {
			return nil, err
		}
		saved = append(saved, p)
	}
	return saved, rows.Err()
}

func (c Reorder) Run(input json.RawMessage) (any, error) {
	var in reorderInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if in.CollectionID == 0 {
		return nil, errors.New("collection id is required")
	}
	for index, id := range in.OrderedIDs {
		if _, err := c.tx.Exec(`UPDATE request_groups SET sort_order = ? WHERE id = ? AND collection_id = ?`,
			index, id, in.CollectionID); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func (c Reorder) Undo(snapshot, _ json.RawMessage) error {
	var saved []position
	if err := json.Unmarshal(snapshot, &saved); err != nil {
		return err
	}
	for _, p := range saved {
		if _, err := c.tx.Exec(`UPDATE request_groups SET sort_order = ? WHERE id = ?`, p.SortOrder, p.ID); err != nil {
			return err
		}
	}
	return nil
}

func (Reorder) Changes(input, _ json.RawMessage) []commands.RowChange {
	return []commands.RowChange{{Component: "group", IDs: commands.IDsOf(input, "ordered_ids")}}
}
