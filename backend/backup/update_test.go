package backup_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"snap-rq/backend/backup"
	"snap-rq/backend/database"
	"snap-rq/backend/secrets"
)

func TestAnUpdateBacksUpOnceBeforeMigrating(t *testing.T) {
	secrets.SetDefault(secrets.NewVault(secrets.MemoryKeyStore()))
	path := filepath.Join(t.TempDir(), "app.db")
	folder := t.TempDir()

	// A brand-new database: nothing to back up.
	db, err := database.OpenWith(path, backup.BeforeUpdate("1.0.0"))
	if err != nil {
		t.Fatal(err)
	}
	settings := backup.DefaultSettings()
	settings.Folder = folder
	if err := backup.SaveSettings(db, settings); err != nil {
		t.Fatal(err)
	}
	// Record 1.0.0 as run, the way the hook does on a later start.
	v, _ := json.Marshal("1.0.0")
	db.Exec(`INSERT OR REPLACE INTO app_settings (key, value) VALUES ('last_run_version', ?)`, string(v))
	db.Close()
	if list, _ := backup.List(folder); len(list) != 0 {
		t.Fatalf("a new database was backed up: %+v", list)
	}

	// Same version again: no backup.
	db, _ = database.OpenWith(path, backup.BeforeUpdate("1.0.0"))
	db.Close()
	if list, _ := backup.List(folder); len(list) != 0 {
		t.Fatal("a start without an update made a backup")
	}

	// Updated: one backup, named after the update.
	db, _ = database.OpenWith(path, backup.BeforeUpdate("1.1.0"))
	db.Close()
	list, _ := backup.List(folder)
	if len(list) != 1 || !strings.Contains(list[0].Header.Reason, "from 1.0.0 to 1.1.0") {
		t.Fatalf("after the update: %+v", list)
	}

	// The next start of 1.1.0 doesn't back up again.
	db, _ = database.OpenWith(path, backup.BeforeUpdate("1.1.0"))
	db.Close()
	if list, _ := backup.List(folder); len(list) != 1 {
		t.Errorf("%d backups, want still 1", len(list))
	}
}
