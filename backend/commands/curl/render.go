package curl

import (
	"database/sql"
	"encoding/json"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// render.go — turn a request definition into an equivalent curl command.

type Render struct{ tx *sql.Tx }

func NewRender(tx *sql.Tx) commands.Command { return &Render{tx: tx} }

func (Render) Info() commands.Info {
	return commands.Info{FunctionID: "curl.render", Label: "Render curl", ReadOnly: true}
}

func (c Render) Run(input json.RawMessage) (any, error) {
	var req models.HttpRequest
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	return RequestToCurl(req), nil
}
