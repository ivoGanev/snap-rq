package backups_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"snap-rq/backend/backup"
	"snap-rq/backend/commands"
	"snap-rq/backend/commands/backups"
	"snap-rq/backend/commands/catalog"
	"snap-rq/backend/database"
	"snap-rq/backend/secrets"
)

func newRunner(t *testing.T) *commands.Runner {
	t.Helper()
	secrets.SetDefault(secrets.NewVault(secrets.MemoryKeyStore()))
	db, err := database.OpenPath(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	r := commands.NewRunner(db)
	if err := r.Register(catalog.All...); err != nil {
		t.Fatal(err)
	}
	return r
}

func run(t *testing.T, r *commands.Runner, name string, in any) (any, error) {
	t.Helper()
	raw, _ := json.Marshal(in)
	return r.Run(name, raw)
}

func TestSettingsKeepThePasswordSealedAndNeverReturnIt(t *testing.T) {
	r := newRunner(t)
	folder := t.TempDir()
	out, err := run(t, r, "backup.update_settings", map[string]any{
		"enabled": true, "folder": folder, "request_delete_threshold": 25, "password": "hunter2",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := out.(backups.SettingsResult)
	if !got.HasPassword || got.PasswordSealed != "" || got.RequestDeleteThreshold != 25 || got.Folder != folder {
		t.Errorf("settings = %+v", got)
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "hunter2") {
		t.Error("the password came back to the frontend")
	}

	// Saving again without a password keeps it; "" removes it.
	out, _ = run(t, r, "backup.update_settings", map[string]any{"enabled": false, "folder": folder})
	if !out.(backups.SettingsResult).HasPassword {
		t.Error("leaving the password out removed it")
	}
	out, _ = run(t, r, "backup.update_settings", map[string]any{"enabled": false, "folder": folder, "password": ""})
	if out.(backups.SettingsResult).HasPassword {
		t.Error(`password "" did not remove it`)
	}
}

func TestDeleteOnlyTouchesBackupsInTheBackupFolder(t *testing.T) {
	r := newRunner(t)
	folder := t.TempDir()
	run(t, r, "backup.update_settings", map[string]any{"enabled": true, "folder": folder})

	outside := filepath.Join(t.TempDir(), "important.snaprq-backup")
	os.WriteFile(outside, []byte("x"), 0o600)
	if _, err := run(t, r, "backup.delete", map[string]any{"path": outside}); err == nil {
		t.Error("deleted a file outside the backup folder")
	}
	notBackup := filepath.Join(folder, "notes.txt")
	os.WriteFile(notBackup, []byte("x"), 0o600)
	if _, err := run(t, r, "backup.delete", map[string]any{"path": notBackup}); err == nil {
		t.Error("deleted a file that isn't a backup")
	}

	inside := filepath.Join(folder, "a"+backup.Extension)
	os.WriteFile(inside, []byte("x"), 0o600)
	if _, err := run(t, r, "backup.delete", map[string]any{"path": inside}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(inside); !os.IsNotExist(err) {
		t.Error("the backup was not deleted")
	}
}
