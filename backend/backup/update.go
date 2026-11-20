package backup

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
)

// update.go — the backup taken when the app starts after an update, before
// the database is migrated: migrations can drop and reshape tables.

const versionKey = "last_run_version"

// BeforeUpdate returns the hook for database.OpenWith. When the stored
// version differs from version it takes a backup (if the app_update trigger
// is on), then stores version. A failed backup is logged, not fatal: the app
// still starts.
func BeforeUpdate(version string) func(*sql.DB) {
	return func(db *sql.DB) {
		var exists int
		if db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'app_settings'`).Scan(&exists); exists == 0 {
			return // a new database: nothing to protect yet
		}
		var raw, last string
		if db.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, versionKey).Scan(&raw) == nil {
			_ = json.Unmarshal([]byte(raw), &last)
		}
		if last == version {
			return
		}

		if s, err := LoadSettings(db); err == nil && s.Wants(TriggerAppUpdate) {
			reason := fmt.Sprintf("before update to %s", version)
			if last != "" {
				reason = fmt.Sprintf("before update from %s to %s", last, version)
			}
			if _, err := Take(db, Automatic, reason); err != nil {
				log.Printf("backup before the update failed: %v", err)
			}
		}

		value, _ := json.Marshal(version)
		if _, err := db.Exec(`INSERT INTO app_settings (key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, versionKey, string(value)); err != nil {
			log.Printf("recording the app version: %v", err)
		}
	}
}
