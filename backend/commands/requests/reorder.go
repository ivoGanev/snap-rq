package requests

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"snap-rq/backend/commands"
)

// reorder.go — persist a custom order for one collection as one bulk
// statement. Snapshot saves the old order values; undo restores them with
// one symmetric bulk statement.

type Reorder struct{ tx *sql.Tx }

func NewReorder(tx *sql.Tx) commands.Command { return &Reorder{tx: tx} }

func (Reorder) Info() commands.Info {
	return commands.Info{FunctionID: "request.update_order", Label: "Reorder Requests"}
}

type reorderInput struct {
	CollectionID int64   `json:"collection_id"`
	OrderedIDs   []int64 `json:"ordered_ids"`
}

// oldOrder is one row's previous position.
type oldOrder struct {
	ID            int64 `json:"id"`
	UserSortOrder int   `json:"user_sort_order"`
}

func (c Reorder) Snapshot(input json.RawMessage) (any, error) {
	var in reorderInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(in.OrderedIDs)), ",")
	args := make([]any, len(in.OrderedIDs))
	for i, id := range in.OrderedIDs {
		args[i] = id
	}
	rows, err := c.tx.Query(
		fmt.Sprintf(`SELECT id, user_sort_order FROM http_requests WHERE id IN (%s)`, placeholders),
		args...)
	if err != nil {
		return nil, fmt.Errorf("loading the current order: %w", err)
	}
	defer rows.Close()

	var saved []oldOrder
	for rows.Next() {
		var o oldOrder
		if err := rows.Scan(&o.ID, &o.UserSortOrder); err != nil {
			return nil, err
		}
		saved = append(saved, o)
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
	if len(in.OrderedIDs) == 0 {
		return nil, nil
	}

	query := `UPDATE http_requests SET user_sort_order = CASE id`
	args := make([]any, 0, len(in.OrderedIDs)*2+1)
	for index, id := range in.OrderedIDs {
		query += " WHEN ? THEN ?"
		args = append(args, id, index)
	}
	query += " ELSE user_sort_order END WHERE collection_id = ?"
	args = append(args, in.CollectionID)

	_, err := c.tx.Exec(query, args...)
	return nil, err
}

// Undo restores the previous order with one bulk statement.
func (c Reorder) Undo(snapshot, _ json.RawMessage) error {
	var saved []oldOrder
	if err := json.Unmarshal(snapshot, &saved); err != nil {
		return err
	}
	query := `UPDATE http_requests SET user_sort_order = CASE id`
	args := make([]any, 0, len(saved)*2)
	for _, o := range saved {
		query += " WHEN ? THEN ?"
		args = append(args, o.ID, o.UserSortOrder)
	}
	query += " ELSE user_sort_order END"

	_, err := c.tx.Exec(query, args...)
	return err
}
