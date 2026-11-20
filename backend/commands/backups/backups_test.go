package backups_test

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// guarded returns a runner with the backup guard on and backups going to a
// fresh folder.
func guarded(t *testing.T) (*commands.Runner, *sql.DB, string) {
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
	folder := t.TempDir()
	if _, err := run(t, r, "backup.update_settings", map[string]any{"enabled": true, "folder": folder}); err != nil {
		t.Fatal(err)
	}
	r.SetBackupGuard(backup.Guard(db, nil))
	return r, db, folder
}

// backupsIn waits for background backups to finish writing. With want 0 it
// just looks: a guard that took no snapshot writes nothing later.
func backupsIn(t *testing.T, folder string, want int) []backup.Info {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		list, _ := backup.List(folder)
		if len(list) >= want || time.Now().After(deadline) {
			return list
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func newCollection(t *testing.T, db *sql.DB, name string, requests int) int64 {
	t.Helper()
	res, err := db.Exec(`INSERT INTO collections (project_id, name) SELECT id, ? FROM projects LIMIT 1`, name)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	for i := 0; i < requests; i++ {
		db.Exec(`INSERT INTO http_requests (collection_id, name, url, method) VALUES (?, 'r', 'http://x', 'GET')`, id)
	}
	return id
}

func TestDeletingACollectionWithRequestsBacksUpFirst(t *testing.T) {
	r, db, folder := guarded(t)
	empty := newCollection(t, db, "Empty", 0)
	full := newCollection(t, db, "Auth", 3)

	if _, err := run(t, r, "collection.delete", map[string]any{"id": empty}); err != nil {
		t.Fatal(err)
	}
	if got := backupsIn(t, folder, 0); len(got) != 0 {
		t.Fatalf("an empty collection made %d backups, want none", len(got))
	}

	if _, err := run(t, r, "collection.delete", map[string]any{"id": full}); err != nil {
		t.Fatal(err)
	}
	got := backupsIn(t, folder, 1)
	if len(got) != 1 || got[0].Header.Kind != backup.Automatic ||
		!strings.Contains(got[0].Header.Reason, `Delete collection "Auth" (3 requests)`) {
		t.Fatalf("backups = %+v", got)
	}

	// The backup holds the collection as it was before the delete.
	restored := filepath.Join(t.TempDir(), "restored.db")
	if _, err := backup.Extract(got[0].Path, restored, db, backup.Unlock{}); err != nil {
		t.Fatal(err)
	}
	old, _ := sql.Open("sqlite", "file:"+restored+"?mode=ro")
	defer old.Close()
	var n int
	old.QueryRow(`SELECT COUNT(*) FROM http_requests WHERE collection_id = ?`, full).Scan(&n)
	if n != 3 {
		t.Errorf("the backup has %d of the collection's requests, want 3", n)
	}
}

func TestEveryRiskyChangeGetsItsOwnBackupEvenRightAfterAnother(t *testing.T) {
	r, db, folder := guarded(t)
	first := newCollection(t, db, "First", 1)
	second := newCollection(t, db, "Second", 1)

	run(t, r, "collection.delete", map[string]any{"id": first})
	backupsIn(t, folder, 1)
	run(t, r, "collection.delete", map[string]any{"id": second})
	got := backupsIn(t, folder, 2)
	if len(got) != 2 {
		t.Fatalf("two deletes in a row made %d backups, want 2 (no throttle)", len(got))
	}
}

func TestRequestDeletesBackUpFromTheThresholdOn(t *testing.T) {
	r, db, folder := guarded(t)
	collection := newCollection(t, db, "Many", 19)
	var ids []int64
	rows, _ := db.Query(`SELECT id FROM http_requests WHERE collection_id = ?`, collection)
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()

	if _, err := run(t, r, "request.delete", map[string]any{"ids": ids[:9]}); err != nil {
		t.Fatal(err)
	}
	if got := backupsIn(t, folder, 0); len(got) != 0 {
		t.Fatalf("9 deletes made a backup; the threshold is 10")
	}
	if _, err := run(t, r, "request.delete", map[string]any{"ids": ids[9:19]}); err != nil {
		t.Fatal(err)
	}
	if got := backupsIn(t, folder, 1); len(got) != 1 {
		t.Fatalf("10 deletes made %d backups, want 1", len(got))
	}
}

func TestSwitchedOffTriggersTakeNoBackup(t *testing.T) {
	r, db, folder := guarded(t)
	run(t, r, "backup.update_settings", map[string]any{
		"enabled": true, "folder": folder, "triggers": map[string]bool{"delete_collection": false},
	})
	collection := newCollection(t, db, "Auth", 3)
	if _, err := run(t, r, "collection.delete", map[string]any{"id": collection}); err != nil {
		t.Fatal(err)
	}
	if got := backupsIn(t, folder, 0); len(got) != 0 {
		t.Errorf("a switched-off trigger made %d backups", len(got))
	}
}
