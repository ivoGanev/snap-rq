package requests

import (
	"database/sql"
	"encoding/json"

	"snap-rq/backend/commands"
)

// get.go — load one request.

type Get struct{ tx *sql.Tx }

func NewGet(tx *sql.Tx) commands.Command { return &Get{tx: tx} }

func (Get) Info() commands.Info {
	return commands.Info{FunctionID: "request.get", Label: "Get Request", ReadOnly: true}
}

func (c Get) Run(input json.RawMessage) (any, error) {
	var in struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	return queryRequest(c.tx, in.ID)
}
