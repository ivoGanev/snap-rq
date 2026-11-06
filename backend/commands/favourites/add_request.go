package favourites

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"snap-rq/backend/commands"
)

// add_request.go — add one request to a favourite collection, appended to
// its order. Undo removes the membership (the created link's ids come back
// in the result).

type AddRequest struct{ tx *sql.Tx }

func NewAddRequest(tx *sql.Tx) commands.Command { return &AddRequest{tx: tx} }

func (AddRequest) Info() commands.Info {
	return commands.Info{FunctionID: "favourite.add_request", Label: "Add To Favourite"}
}

// Snapshot: the membership did not exist before.
func (AddRequest) Snapshot(_ json.RawMessage) (any, error) { return nil, nil }

func (c AddRequest) Run(input json.RawMessage) (any, error) {
	var in struct {
		FavouriteCollectionID int64 `json:"favourite_collection_id"`
		RequestID             int64 `json:"request_id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}

	var nextSort int
	if err := c.tx.QueryRow(
		`SELECT COALESCE(MAX(user_sort_order), 0) + 1 FROM favourite_items
		 WHERE favourite_collection_id = ?`, in.FavouriteCollectionID,
	).Scan(&nextSort); err != nil {
		return nil, fmt.Errorf("computing sort order: %w", err)
	}

	result, err := c.tx.Exec(
		`INSERT INTO favourite_items (favourite_collection_id, http_request_id, user_sort_order)
		 VALUES (?, ?, ?)`,
		in.FavouriteCollectionID, in.RequestID, nextSort)
	if err != nil {
		return nil, fmt.Errorf("adding favourite item: %w", err)
	}
	id, _ := result.LastInsertId()
	return map[string]any{
		"id": id, "favourite_collection_id": in.FavouriteCollectionID,
		"http_request_id": in.RequestID, "user_sort_order": nextSort,
	}, nil
}

// Undo removes the created membership.
func (c AddRequest) Undo(_, result json.RawMessage) error {
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(result, &created); err != nil {
		return err
	}
	_, err := c.tx.Exec(`DELETE FROM favourite_items WHERE id = ?`, created.ID)
	return err
}

func (AddRequest) Changes(_, result json.RawMessage) []commands.RowChange {
	return []commands.RowChange{{Component: "favourite", IDs: commands.IDsOf(result, "favourite_collection_id")}}
}
