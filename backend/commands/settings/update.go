package settings

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// update.go — persist the settings. Zero timeout means the default; values
// outside the allowed ranges are rejected.

type Update struct{ tx *sql.Tx }

func NewUpdate(tx *sql.Tx) commands.Command { return &Update{tx: tx} }

func (Update) Info() commands.Info {
	return commands.Info{FunctionID: "settings.update", Label: "Update Settings"}
}

func (c Update) Run(input json.RawMessage) (any, error) {
	var s models.AppSettings
	if err := json.Unmarshal(input, &s); err != nil {
		return nil, err
	}
	// A caller that predates the cookie jar setting leaves it out; keep the
	// stored choice then, instead of reading the missing field as "off".
	var optional struct {
		UseCookieJar *bool `json:"use_cookie_jar"`
	}
	if err := json.Unmarshal(input, &optional); err != nil {
		return nil, err
	}
	if s.RequestTimeoutSeconds == 0 {
		s.RequestTimeoutSeconds = 30
	}
	if s.RequestTimeoutSeconds < 1 || s.RequestTimeoutSeconds > 99999 {
		return nil, fmt.Errorf("request timeout must be between 1 and 99999 seconds")
	}
	if s.UndoHistoryLimit == 0 {
		s.UndoHistoryLimit = 100
	}
	if s.UndoHistoryLimit < 10 || s.UndoHistoryLimit > 1000 {
		return nil, fmt.Errorf("undo history limit must be between 10 and 1000")
	}

	write := func(key string, value any) error {
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		_, err = c.tx.Exec(
			`INSERT INTO app_settings (key, value) VALUES (?, ?)
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, string(raw))
		return err
	}
	if err := write("validate_ssl_certificates", s.ValidateSSLCertificates); err != nil {
		return nil, err
	}
	if err := write("request_timeout_seconds", s.RequestTimeoutSeconds); err != nil {
		return nil, err
	}
	if err := write("undo_history_limit", s.UndoHistoryLimit); err != nil {
		return nil, err
	}
	if optional.UseCookieJar != nil {
		if err := write("use_cookie_jar", s.UseCookieJar); err != nil {
			return nil, err
		}
	} else {
		s.UseCookieJar = c.storedUseCookieJar()
	}
	return s, nil
}

// storedUseCookieJar reads the saved cookie jar choice; on by default.
func (c Update) storedUseCookieJar() bool {
	enabled := true
	var raw string
	if err := c.tx.QueryRow(`SELECT value FROM app_settings WHERE key = 'use_cookie_jar'`).Scan(&raw); err == nil {
		_ = json.Unmarshal([]byte(raw), &enabled)
	}
	return enabled
}
