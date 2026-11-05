package favourites

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"snap-rq/backend/commands"
)

// reorder.go — persist a custom order for one favourite collection as one
// bulk statement. Snapshot saves the old order; undo restores it with one
// symmetric bulk statement.

type Reorder struct{ tx *sql.Tx }

func NewReorder(tx *sql.Tx) commands.Command { return &Reorder{tx: tx} }

func (Reorder) Info() commands.Info {
	return commands.Info{FunctionID: "favourite.reorder", Label: "Reorder Favourites"}
}

type oldOrder struct {
	ID            int64 `json:"id"`
	UserSortOrder int   `json:"user_sort_order"`
}

func (c Reorder) Snapshot(input json.RawMessage) (any, error) {
	var in struct {
		FavouriteCollectionID int64   `json:"favourite_collection_id"`
		OrderedRequestIDs     []int64 `json:"ordered_request_ids"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	rows, err := c.tx.Query(`
		SELECT http_request_id, user_sort_order FROM favourite_items
		WHERE favourite_collection_id = ?`, in.FavouriteCollectionID)
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
	var in struct {
		FavouriteCollectionID int64   `json:"favourite_collection_id"`
		OrderedRequestIDs     []int64 `json:"ordered_request_ids"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if in.FavouriteCollectionID == 0 || len(in.OrderedRequestIDs) == 0 {
		return nil, nil
	}

	query := `UPDATE favourite_items SET user_sort_order = CASE http_request_id`
	args := make([]any, 0, len(in.OrderedRequestIDs)*2+1)
	for index, id := range in.OrderedRequestIDs {
		query += " WHEN ? THEN ?"
		args = append(args, id, index)
	}
	query += " ELSE user_sort_order END WHERE favourite_collection_id = ?"
	args = append(args, in.FavouriteCollectionID)

	_, err := c.tx.Exec(query, args...)
	return nil, err
}

func (c Reorder) Undo(snapshot, _ json.RawMessage) error {
	var saved []oldOrder
	if err := json.Unmarshal(snapshot, &saved); err != nil {
		return err
	}
	query := `UPDATE favourite_items SET user_sort_order = CASE http_request_id`
	args := make([]any, 0, len(saved)*2)
	for _, o := range saved {
		query += " WHEN ? THEN ?"
		args = append(args, o.ID, o.UserSortOrder)
	}
	query += " ELSE user_sort_order END"

	_, err := c.tx.Exec(query, args...)
	return err
}

func (Reorder) Changes(input, _ json.RawMessage) []commands.RowChange {
	return []commands.RowChange{{Component: "favourite", IDs: commands.IDsOf(input, "favourite_collection_id")}}
}

var _ = strings.TrimSpace
