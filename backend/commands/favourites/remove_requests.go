package favourites

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"snap-rq/backend/commands"
)

// remove_requests.go — remove requests from a favourite collection, in
// bulk. Snapshot saves the memberships; undo re-inserts them under their
// original ids.

type RemoveRequests struct{ tx *sql.Tx }

func NewRemoveRequests(tx *sql.Tx) commands.Command { return &RemoveRequests{tx: tx} }

func (RemoveRequests) Info() commands.Info {
	return commands.Info{FunctionID: "favourite.remove_requests", Label: "Remove From Favourite"}
}

type removeInput struct {
	FavouriteCollectionID int64   `json:"favourite_collection_id"`
	RequestIDs            []int64 `json:"request_ids"`
}

// membership is one favourite_items row, as saved for undo.
type membership struct {
	ID                    int64   `json:"id"`
	FavouriteCollectionID int64   `json:"favourite_collection_id"`
	HTTPRequestID         int64   `json:"http_request_id"`
	AddedAt               string  `json:"added_at"`
	UserSortOrder         int     `json:"user_sort_order"`
	UserColor             *string `json:"user_color"`
}

func (c RemoveRequests) Snapshot(input json.RawMessage) (any, error) {
	var in removeInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}

	rows, err := c.tx.Query(`
		SELECT id, favourite_collection_id, http_request_id, added_at, user_sort_order, user_color
		FROM favourite_items
		WHERE favourite_collection_id = ? AND http_request_id IN (`+placeholders(len(in.RequestIDs))+`)`,
		append([]any{in.FavouriteCollectionID}, int64Args(in.RequestIDs)...)...)
	if err != nil {
		return nil, fmt.Errorf("loading memberships: %w", err)
	}
	defer rows.Close()

	var saved []membership
	for rows.Next() {
		var m membership
		if err := rows.Scan(&m.ID, &m.FavouriteCollectionID, &m.HTTPRequestID,
			&m.AddedAt, &m.UserSortOrder, &m.UserColor); err != nil {
			return nil, err
		}
		saved = append(saved, m)
	}
	return saved, rows.Err()
}

func (c RemoveRequests) Run(input json.RawMessage) (any, error) {
	var in removeInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if len(in.RequestIDs) == 0 {
		return nil, nil
	}

	_, err := c.tx.Exec(`
		DELETE FROM favourite_items
		WHERE favourite_collection_id = ? AND http_request_id IN (`+placeholders(len(in.RequestIDs))+`)`,
		append([]any{in.FavouriteCollectionID}, int64Args(in.RequestIDs)...)...)
	return nil, err
}

// Undo re-inserts the removed memberships under their original ids.
func (c RemoveRequests) Undo(snapshot, _ json.RawMessage) error {
	var saved []membership
	if err := json.Unmarshal(snapshot, &saved); err != nil {
		return err
	}
	for _, m := range saved {
		if _, err := c.tx.Exec(`
			INSERT INTO favourite_items
			  (id, favourite_collection_id, http_request_id, added_at, user_sort_order, user_color)
			VALUES (?, ?, ?, ?, ?, ?)`,
			m.ID, m.FavouriteCollectionID, m.HTTPRequestID, m.AddedAt, m.UserSortOrder, m.UserColor,
		); err != nil {
			return err
		}
	}
	return nil
}

func (RemoveRequests) Changes(input, _ json.RawMessage) []commands.RowChange {
	return []commands.RowChange{{Component: "favourite", IDs: commands.IDsOf(input, "favourite_collection_id")}}
}

// placeholders and int64Args are the tiny SQL-string helpers this package
// uses; they live here because several files share them.
func placeholders(n int) string {
	out := ""
	for i := 0; i < n; i++ {
		if i > 0 {
			out += ","
		}
		out += "?"
	}
	return out
}

func int64Args(ids []int64) []any {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return args
}
