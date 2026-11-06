package responses

import (
	"database/sql"
	"encoding/json"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// save.go — store one response; the request row is not touched here.

type Save struct{ tx *sql.Tx }

func NewSave(tx *sql.Tx) commands.Command { return &Save{tx: tx} }

func (Save) Info() commands.Info {
	return commands.Info{FunctionID: "response.save", Label: "Save Response"}
}

func (c Save) Run(input json.RawMessage) (any, error) {
	var resp models.HttpResponse
	if err := json.Unmarshal(input, &resp); err != nil {
		return nil, err
	}
	return insertResponse(c.tx, resp)
}
