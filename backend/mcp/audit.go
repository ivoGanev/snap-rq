package mcp

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// The audit log records every tool call an AI client makes, so the user can
// see what happened. It stores a short summary and the arguments after
// masking, never results and never the token. See docs/mcp.md.

const (
	auditOK      = "ok"
	auditError   = "error"
	auditRefused = "refused" // the tool is switched off, or the input held a masked value

	auditMaxRows    = 1000
	auditMaxAge     = 30 * 24 * time.Hour
	auditMaxArgs    = 2000
	auditMaxSummary = 160
)

// AuditEntry is one tool call.
type AuditEntry struct {
	ID         int64  `json:"id"`
	At         int64  `json:"at"` // unix milliseconds
	Tool       string `json:"tool"`
	Client     string `json:"client"`
	Status     string `json:"status"`
	Summary    string `json:"summary"`
	Args       string `json:"args"`
	Error      string `json:"error"`
	DurationMs int64  `json:"duration_ms"`
}

// AddAudit stores an entry and prunes old ones.
func (s *Store) AddAudit(e AuditEntry) error {
	if _, err := s.db.Exec(
		`INSERT INTO mcp_audit_log (at, tool, client, status, summary, args, error, duration_ms)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.At, e.Tool, e.Client, e.Status, e.Summary, e.Args, e.Error, e.DurationMs,
	); err != nil {
		return err
	}
	cutoff := time.Now().Add(-auditMaxAge).UnixMilli()
	if _, err := s.db.Exec(`DELETE FROM mcp_audit_log WHERE at < ?`, cutoff); err != nil {
		return err
	}
	// ids only grow, so the newest auditMaxRows rows are those above max-auditMaxRows.
	_, err := s.db.Exec(
		`DELETE FROM mcp_audit_log WHERE id <= (SELECT COALESCE(MAX(id), 0) FROM mcp_audit_log) - ?`,
		auditMaxRows)
	return err
}

// RecentAudit returns the newest entries first.
func (s *Store) RecentAudit(limit int) ([]AuditEntry, error) {
	if limit < 1 || limit > 200 {
		limit = 100
	}
	rows, err := s.db.Query(
		`SELECT id, at, tool, client, status, summary, args, error, duration_ms
		 FROM mcp_audit_log ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.At, &e.Tool, &e.Client, &e.Status, &e.Summary, &e.Args, &e.Error, &e.DurationMs); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ClearAudit deletes the whole log.
func (s *Store) ClearAudit() error {
	_, err := s.db.Exec(`DELETE FROM mcp_audit_log`)
	return err
}

// summarize describes a call in one line without the content: arrays become
// counts, short scalars are shown, long text is left to the (masked) args.
func summarize(in any) string {
	generic, err := toGeneric(in)
	if err != nil {
		return ""
	}
	obj, ok := generic.(map[string]any)
	if !ok {
		return ""
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		switch v := obj[k].(type) {
		case []any:
			parts = append(parts, fmt.Sprintf("%s×%d", k, len(v)))
		case json.Number:
			parts = append(parts, fmt.Sprintf("%s=%s", k, v))
		case bool:
			parts = append(parts, fmt.Sprintf("%s=%t", k, v))
		case string:
			if k != "value" && len(v) <= 40 {
				parts = append(parts, fmt.Sprintf("%s=%s", k, v))
			}
		}
	}
	return truncate(strings.Join(parts, " "), auditMaxSummary)
}

// maskArgs returns the call's arguments as JSON with secrets hidden. It
// always masks, whatever the output setting says.
func maskArgs(m *Masker, in any) string {
	generic, err := toGeneric(in)
	if err != nil {
		return ""
	}
	if m == nil {
		m = NewMasker(nil)
	}
	generic = m.Tree(generic)
	if obj, ok := generic.(map[string]any); ok {
		if v, ok := obj["value"].(string); ok {
			obj["value"] = m.Text(v)
		}
	}
	raw, err := json.Marshal(generic)
	if err != nil {
		return ""
	}
	return truncate(string(raw), auditMaxArgs)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && s[cut]&0xC0 == 0x80 { // do not split a UTF-8 character
		cut--
	}
	return s[:cut] + "…"
}
