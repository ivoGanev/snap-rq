package favourites

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"snap-rq/backend/commands"
)

// list_memberships.go — which favourite collections contain the given
// requests. Two commands in one file: the single and the bulk form, both
// reads.

type ListMemberships struct{ tx *sql.Tx }

func NewListMemberships(tx *sql.Tx) commands.Command { return &ListMemberships{tx: tx} }

func (ListMemberships) Info() commands.Info {
	return commands.Info{FunctionID: "favourite.list_memberships", Label: "List Memberships", ReadOnly: true}
}

func (c ListMemberships) Run(input json.RawMessage) (any, error) {
	var in struct {
		RequestIDs []int64 `json:"request_ids"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if len(in.RequestIDs) == 0 {
		return map[string][]int64{}, nil
	}

	// one row per (request, favourite collection) pair, returned as a map
	// keyed by the stringified request id
	ph := strings.TrimSuffix(strings.Repeat("?,", len(in.RequestIDs)), ",")
	args := make([]any, len(in.RequestIDs))
	for i, id := range in.RequestIDs {
		args[i] = id
	}
	rows, err := c.tx.Query(`
		SELECT http_request_id, favourite_collection_id FROM favourite_items
		WHERE http_request_id IN (`+ph+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("listing memberships: %w", err)
	}
	defer rows.Close()

	result := map[string][]int64{}
	for rows.Next() {
		var requestID, collectionID int64
		if err := rows.Scan(&requestID, &collectionID); err != nil {
			return nil, err
		}
		key := fmt.Sprintf("%d", requestID)
		result[key] = append(result[key], collectionID)
	}
	for _, id := range in.RequestIDs {
		key := fmt.Sprintf("%d", id)
		if _, ok := result[key]; !ok {
			result[key] = []int64{}
		}
	}
	return result, rows.Err()
}
