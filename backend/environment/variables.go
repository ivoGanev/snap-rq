// Package environment fills a request's {{placeholders}} from an
// environment's variables. It is the backend's one place for this: sending,
// trying a script and copying as cURL all read an environment through it.
package environment

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"snap-rq/backend/models"
	"snap-rq/backend/secrets"
)

// Querier is a database or a transaction.
type Querier interface {
	secrets.Querier
	Query(query string, args ...any) (*sql.Rows, error)
}

// Variables is one environment's variables, read once from the database.
// Secrets are decrypted the first time they are used and remembered, so
// whatever was revealed can be masked in logs and error messages.
type Variables struct {
	q        Querier
	plain    map[string]string
	secret   map[string]string // key → stored (encrypted) value
	revealed map[string]string
	locked   map[string]bool
}

// Load reads the environment's variables; environment 0 (none) has none.
func Load(q Querier, environmentID int64) (*Variables, error) {
	v := &Variables{q: q, plain: map[string]string{}, secret: map[string]string{},
		revealed: map[string]string{}, locked: map[string]bool{}}
	if environmentID == 0 {
		return v, nil
	}
	rows, err := q.Query(`SELECT key, value, secret FROM environment_variables WHERE environment_id = ?`, environmentID)
	if err != nil {
		return nil, fmt.Errorf("loading variables: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		var secret bool
		if err := rows.Scan(&key, &value, &secret); err != nil {
			return nil, err
		}
		switch {
		case !secret:
			v.plain[key] = value
		case value != "":
			v.secret[key] = value
		}
	}
	return v, rows.Err()
}

// Get is a variable's value; a secret the vault can't open counts as unset.
// It is ctx.env for scripts.
func (v *Variables) Get(key string) (string, bool) {
	if value, ok := v.plain[key]; ok {
		return value, true
	}
	if value, ok := v.revealed[key]; ok {
		return value, true
	}
	encrypted, ok := v.secret[key]
	if !ok || v.locked[key] {
		return "", false
	}
	value, err := secrets.Default().Decrypt(v.q, encrypted)
	if err != nil {
		v.locked[key] = true
		return "", false
	}
	v.revealed[key] = value
	return value, true
}

// Revealed is every secret value used so far, by name, for masking.
func (v *Variables) Revealed() map[string]string { return v.revealed }

// Fill replaces the {{placeholders}} it has values for. Unknown names stay as
// they are, and so do secrets the vault can't open: those are returned.
func (v *Variables) Fill(text string) (string, []string) {
	if !strings.Contains(text, "{{") {
		return text, nil
	}
	var locked []string
	filled := models.VariablePlaceholder.ReplaceAllStringFunc(text, func(match string) string {
		key := models.VariablePlaceholder.FindStringSubmatch(match)[1]
		if value, ok := v.Get(key); ok {
			return value
		}
		if v.locked[key] {
			locked = append(locked, key)
		}
		return match
	})
	return filled, locked
}

// FillRequest fills the URL, headers, body and form-data text fields; file
// paths are left as they are.
func (v *Variables) FillRequest(req models.HttpRequest) (models.HttpRequest, []string) {
	var locked, more []string
	req.URL, locked = v.Fill(req.URL)
	req.RequestHeaders, more = v.Fill(req.RequestHeaders)
	locked = append(locked, more...)
	req.Body, more = v.Fill(req.Body)
	locked = append(locked, more...)
	req.BodyForm, more = v.fillFormText(req.BodyForm)
	return req, append(locked, more...)
}

// fillFormText fills the values of a form-data body's text rows, keeping the
// rows' other fields as stored.
func (v *Variables) fillFormText(raw string) (string, []string) {
	if !strings.Contains(raw, "{{") {
		return raw, nil
	}
	var rows []map[string]any
	if json.Unmarshal([]byte(raw), &rows) != nil {
		return raw, nil
	}
	var locked []string
	for _, row := range rows {
		value, isText := row["value"].(string)
		if row["type"] == "file" || !isText {
			continue
		}
		filled, more := v.Fill(value)
		row["value"] = filled
		locked = append(locked, more...)
	}
	out, err := json.Marshal(rows)
	if err != nil {
		return raw, nil
	}
	return string(out), locked
}
