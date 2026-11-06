package requests

import (
	"database/sql"
	"encoding/json"

	"snap-rq/backend/commands"
)

// list_for_project.go — every request of one project.

type ListForProject struct{ tx *sql.Tx }

func NewListForProject(tx *sql.Tx) commands.Command { return &ListForProject{tx: tx} }

func (ListForProject) Info() commands.Info {
	return commands.Info{FunctionID: "request.list_for_project", Label: "List Project Requests", ReadOnly: true}
}

func (c ListForProject) Run(input json.RawMessage) (any, error) {
	var in struct {
		ProjectID int64 `json:"project_id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	return querySummaries(c.tx, `WHERE c.project_id = ? ORDER BY hr.name`, in.ProjectID)
}
