// Package settings holds the app settings commands. Settings are
// preferences, not workspace state — changes are not undoable and never
// disturb the undo log.
package settings

import (
	"database/sql"
	"encoding/json"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// get.go — read the current settings, with secure defaults.

type Get struct{ tx *sql.Tx }

func NewGet(tx *sql.Tx) commands.Command { return &Get{tx: tx} }

func (Get) Info() commands.Info {
	return commands.Info{FunctionID: "settings.get", Label: "Get Settings", ReadOnly: true}
}

func (c Get) Run(_ json.RawMessage) (any, error) {
	s := models.AppSettings{
		ValidateSSLCertificates: true,
		RequestTimeoutSeconds:   30,
		UndoHistoryLimit:        100,
	}
	read := func(key string, dest any) {
		var raw string
		if err := c.tx.QueryRow(
			`SELECT value FROM app_settings WHERE key = ?`, key).Scan(&raw); err == nil {
			_ = json.Unmarshal([]byte(raw), dest)
		}
	}
	read("validate_ssl_certificates", &s.ValidateSSLCertificates)
	read("request_timeout_seconds", &s.RequestTimeoutSeconds)
	read("undo_history_limit", &s.UndoHistoryLimit)
	if s.RequestTimeoutSeconds < 1 || s.RequestTimeoutSeconds > 99999 {
		s.RequestTimeoutSeconds = 30
	}
	if s.UndoHistoryLimit < 10 || s.UndoHistoryLimit > 1000 {
		s.UndoHistoryLimit = 100
	}
	return s, nil
}
