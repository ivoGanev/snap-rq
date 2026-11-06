package requests

import (
	"database/sql"
	"encoding/json"

	"snap-rq/backend/commands"
)

// list_recent.go — a project's most recently used requests.

type ListRecent struct{ tx *sql.Tx }

func NewListRecent(tx *sql.Tx) commands.Command { return &ListRecent{tx: tx} }

func (ListRecent) Info() commands.Info {
	return commands.Info{FunctionID: "request.list_recent", Label: "List Recent Requests", ReadOnly: true}
}

func (c ListRecent) Run(input json.RawMessage) (any, error) {
	var in struct {
		ProjectID int64 `json:"project_id"`
		Limit     int   `json:"limit"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if in.Limit <= 0 {
		in.Limit = 50
	}
	if in.Limit > 100 {
		in.Limit = 100
	}
	return querySummaries(c.tx,
		`WHERE c.project_id = ? AND hr.last_used_at > 0
		 ORDER BY hr.last_used_at DESC, hr.name ASC LIMIT ?`,
		in.ProjectID, in.Limit)
}
