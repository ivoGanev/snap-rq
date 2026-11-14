package cookiejar

import (
	"database/sql"
	"encoding/json"

	"snap-rq/backend/commands"
	"snap-rq/backend/cookies"
	"snap-rq/backend/models"
)

// list.go — a project's cookies, ordered by domain, path and name.

type List struct{ tx *sql.Tx }

func NewList(tx *sql.Tx) commands.Command { return &List{tx: tx} }

func (List) Info() commands.Info {
	return commands.Info{FunctionID: "cookie.list", Label: "List Cookies", ReadOnly: true}
}

func (c List) Run(input json.RawMessage) (any, error) {
	var in struct {
		ProjectID int64 `json:"project_id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	list, err := cookies.List(c.tx, in.ProjectID)
	if list == nil {
		list = []models.Cookie{}
	}
	return list, err
}
