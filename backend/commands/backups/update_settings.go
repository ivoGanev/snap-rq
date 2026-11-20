package backups

import (
	"database/sql"
	"encoding/json"
	"path/filepath"

	"snap-rq/backend/backup"
	"snap-rq/backend/commands"
)

// update_settings.go — change the backup settings. Not undoable, like the
// app settings. The password is optional: left out it stays; "" removes it.

type UpdateSettings struct{ tx *sql.Tx }

func NewUpdateSettings(tx *sql.Tx) commands.Command { return &UpdateSettings{tx: tx} }

func (UpdateSettings) Info() commands.Info {
	return commands.Info{FunctionID: "backup.update_settings", Label: "Update Backup Settings"}
}

func (c UpdateSettings) Run(input json.RawMessage) (any, error) {
	var in struct {
		backup.Settings
		Password *string `json:"password"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	current, err := backup.LoadSettings(c.tx)
	if err != nil {
		return nil, err
	}

	next := in.Settings
	next.PasswordSealed = current.PasswordSealed
	if in.Password != nil {
		if err := next.SetPassword(c.tx, *in.Password); err != nil {
			return nil, err
		}
	}
	// The default folder is stored as "", so it follows the OS if it moves.
	if next.Folder != "" && filepath.Clean(next.Folder) == filepath.Clean(backup.DefaultFolder()) {
		next.Folder = ""
	}
	if err := backup.SaveSettings(c.tx, next); err != nil {
		return nil, err
	}
	saved, err := backup.LoadSettings(c.tx)
	if err != nil {
		return nil, err
	}
	return SettingsResult{Settings: saved.Public(), DefaultFolder: backup.DefaultFolder(), AllTriggers: backup.AllTriggers}, nil
}
