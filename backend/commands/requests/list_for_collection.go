package requests

import (
	"database/sql"
	"encoding/json"

	"snap-rq/backend/commands"
)

// list_for_collection.go — one collection's summaries in the user's order.

type ListForCollection struct{ tx *sql.Tx }

func NewListForCollection(tx *sql.Tx) commands.Command { return &ListForCollection{tx: tx} }

func (ListForCollection) Info() commands.Info {
	return commands.Info{FunctionID: "request.list_for_collection", Label: "List Collection Requests", ReadOnly: true}
}

func (c ListForCollection) Run(input json.RawMessage) (any, error) {
	var in struct {
		CollectionID int64 `json:"collection_id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	return querySummaries(c.tx,
		`WHERE hr.collection_id = ? ORDER BY hr.user_sort_order, hr.name`, in.CollectionID)
}
