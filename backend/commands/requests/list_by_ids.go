package requests

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// list_by_ids.go — targeted summary fetch; ids that no longer exist are
// skipped. Used by the frontend's store sync after commands.

type ListByIDs struct{ tx *sql.Tx }

func NewListByIDs(tx *sql.Tx) commands.Command { return &ListByIDs{tx: tx} }

func (ListByIDs) Info() commands.Info {
	return commands.Info{FunctionID: "request.list_by_ids", Label: "List Requests By Ids", ReadOnly: true}
}

func (c ListByIDs) Run(input json.RawMessage) (any, error) {
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if len(in.IDs) == 0 {
		return []models.HttpRequestSummary{}, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(in.IDs)), ",")
	args := make([]any, len(in.IDs))
	for i, id := range in.IDs {
		args[i] = id
	}
	return querySummaries(c.tx,
		fmt.Sprintf(`WHERE hr.id IN (%s)`, placeholders), args...)
}
