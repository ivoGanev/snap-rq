package requests

import (
	"database/sql"
	"encoding/json"

	"snap-rq/backend/commands"
)

// list_summaries.go — every request as a lightweight summary.

type ListSummaries struct{ tx *sql.Tx }

func NewListSummaries(tx *sql.Tx) commands.Command { return &ListSummaries{tx: tx} }

func (ListSummaries) Info() commands.Info {
	return commands.Info{FunctionID: "request.list_summaries", Label: "List Requests", ReadOnly: true}
}

func (c ListSummaries) Run(_ json.RawMessage) (any, error) {
	return querySummaries(c.tx, `ORDER BY hr.name`)
}
