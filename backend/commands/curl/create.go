package curl

import (
	"database/sql"
	"encoding/json"

	"snap-rq/backend/commands"
	"snap-rq/backend/protocol/httpreq"
	"snap-rq/backend/repository"
)

// create.go — "Copy as cURL": a stored request as it would be sent, with its
// project's environment filled in, secrets included. Not an MCP tool: its
// output holds real secret values.

type Create struct{ tx *sql.Tx }

func NewCreate(tx *sql.Tx) commands.Command { return &Create{tx: tx} }

func (Create) Info() commands.Info {
	return commands.Info{FunctionID: "curl.create", Label: "Copy as cURL", ReadOnly: true}
}

func (c Create) Run(input json.RawMessage) (any, error) {
	var in struct {
		RequestID int64 `json:"request_id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	req, err := repository.NewRequests(c.tx).Get(in.RequestID)
	if err != nil {
		return nil, err
	}
	env, err := repository.NewEnvironments(c.tx).Current(req.ProjectID,
		repository.Load{Variables: true, Secrets: repository.Decrypt})
	if err != nil {
		return nil, err
	}
	// A locked secret stays a {{placeholder}}: the copy still works.
	req, _ = httpreq.Resolve(req, env)
	out, err := httpreq.Build(req)
	if err != nil {
		return nil, err
	}
	return out.Curl(), nil
}
