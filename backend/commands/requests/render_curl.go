package requests

import (
	"database/sql"
	"encoding/json"
	"maps"
	"strings"

	"snap-rq/backend/commands/curl"
	"snap-rq/backend/models"
)

// CurlService gives the frontend a request as a curl command, with the
// environment's variables filled in the way a send fills them, secrets
// included. A bound service rather than a command: it's a lookup, and MCP,
// which only reaches commands, must never see its real secret values.
type CurlService struct{ db *sql.DB }

func NewCurlService(db *sql.DB) *CurlService { return &CurlService{db: db} }

// Render fills in plain values and the environment's secrets; unknown
// placeholders, and secrets the vault can't open, stay {{placeholders}}.
// Scripts don't run.
func (s *CurlService) Render(request models.HttpRequest, plain map[string]string, environmentID int64) (string, error) {
	secretValues, _, err := lookupSecrets(s.db, environmentID, request)
	if err != nil {
		return "", err
	}
	values := make(map[string]string, len(plain)+len(secretValues))
	maps.Copy(values, plain)
	maps.Copy(values, secretValues)

	req := fillIn(request, values)
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
