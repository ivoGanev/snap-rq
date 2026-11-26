package curl

import (
	"database/sql"
	"encoding/json"
	"slices"
	"strings"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
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
	req, _ = httpreq.New(env).Resolve(req)
	built, err := httpreq.Build(req)
	if err != nil {
		return nil, err
	}
	return FromOutgoing(built), nil
}

// FromOutgoing writes the curl command for a request as it goes out.
func FromOutgoing(b httpreq.Outgoing) string {
	names := make([]string, 0, len(b.Header))
	for name := range b.Header {
		names = append(names, name)
	}
	slices.Sort(names)
	var lines []string
	for _, name := range names {
		for _, value := range b.Header[name] {
			lines = append(lines, name+": "+value)
		}
	}
	req := models.HttpRequest{Method: b.Method, URL: b.URL, RequestHeaders: strings.Join(lines, "\n"), Body: b.Body}
	if b.FormData {
		form, _ := json.Marshal(b.Form)
		req.BodyType, req.BodyForm = "form-data", string(form)
	}
	return RequestToCurl(req)
}
