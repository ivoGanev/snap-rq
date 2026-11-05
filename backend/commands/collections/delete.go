package collections

import (
	"database/sql"
	"encoding/json"
	"errors"

	"snap-rq/backend/commands"
)

// delete.go — remove a collection with everything the cascades take: its
// appearance, requests, responses, tag and favourite memberships.

type Delete struct{ tx *sql.Tx }

func NewDelete(tx *sql.Tx) commands.Command { return &Delete{tx: tx} }

func (Delete) Info() commands.Info {
	return commands.Info{FunctionID: "collection.delete", Label: "Delete Collection"}
}

func (c Delete) Run(input json.RawMessage) (any, error) {
	var in struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if in.ID == 0 {
		return nil, errors.New("collection id is required")
	}

	// the requests that die with the collection, so the UI can drop exactly
	// those rows from its store
	requestIDs, err := selectRequestIDs(c.tx, in.ID)
	if err != nil {
		return nil, err
	}

	if _, err := c.tx.Exec(`DELETE FROM collections WHERE id = ?`, in.ID); err != nil {
		return nil, err
	}
	return map[string]any{"request_ids": requestIDs}, nil
}

// selectRequestIDs lists the requests in a collection.
func selectRequestIDs(tx *sql.Tx, collectionID int64) ([]int64, error) {
	rows, err := tx.Query(`SELECT id FROM http_requests WHERE collection_id = ?`, collectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
