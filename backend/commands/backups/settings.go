// Package backups holds the backup commands, one operation per file. The
// engine lives in backend/backup; see docs/plans/backups.md.
package backups

import (
	"database/sql"
	"encoding/json"

	"snap-rq/backend/backup"
	"snap-rq/backend/commands"
)

// settings.go — the backup settings, for Settings → Backups.

type Settings struct{ tx *sql.Tx }

func NewSettings(tx *sql.Tx) commands.Command { return &Settings{tx: tx} }

func (Settings) Info() commands.Info {
	return commands.Info{FunctionID: "backup.settings", Label: "Backup Settings", ReadOnly: true}
}

// SettingsResult adds what the page shows next to the settings.
type SettingsResult struct {
	backup.Settings
	DefaultFolder string           `json:"default_folder"`
	AllTriggers   []backup.Trigger `json:"all_triggers"`
}

func (c Settings) Run(json.RawMessage) (any, error) {
	s, err := backup.LoadSettings(c.tx)
	if err != nil {
		return nil, err
	}
	return SettingsResult{Settings: s.Public(), DefaultFolder: backup.DefaultFolder(), AllTriggers: backup.AllTriggers}, nil
}
