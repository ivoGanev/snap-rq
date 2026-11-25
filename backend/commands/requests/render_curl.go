package requests

import (
	"database/sql"
	"encoding/json"
	"maps"
	"strings"

	"snap-rq/backend/commands"
	"snap-rq/backend/commands/curl"
)

// RenderCurl writes a request as a curl command with the environment's
// variables filled in the way a send fills them, secrets included. Scripts
// don't run. MCP may never call it (see mcp.NeverExposed): its output holds
// real secret values.
type RenderCurl struct{ tx *sql.Tx }

func NewRenderCurl(tx *sql.Tx) commands.Command { return &RenderCurl{tx: tx} }

func (RenderCurl) Info() commands.Info {
	return commands.Info{FunctionID: "request.render_curl", Label: "Copy as cURL", ReadOnly: true}
}

func (c RenderCurl) Run(input json.RawMessage) (any, error) {
	var in sendInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	// A locked secret stays a {{placeholder}}: the copy still works.
	secretValues, _, err := lookupSecrets(c.tx, in.EnvironmentID, in.Request)
	if err != nil {
		return nil, err
	}
	values := in.plainValues()
	maps.Copy(values, secretValues)

	req := fillIn(in.Request, values)
	req.BodyForm = fillInFormText(req.BodyForm, values)
	return curl.RequestToCurl(req), nil
}

// fillInFormText fills in the text fields of a form-data body; file paths
// are left as they are, as when sending.
func fillInFormText(raw string, values map[string]string) string {
	if !strings.Contains(raw, "{{") {
		return raw
	}
	var entries []formEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return raw
	}
	for i, entry := range entries {
		if entry.Type != "file" {
			entries[i].Value = interpolate(entry.Value, values)
		}
	}
	out, err := json.Marshal(entries)
	if err != nil {
		return raw
	}
	return string(out)
}
