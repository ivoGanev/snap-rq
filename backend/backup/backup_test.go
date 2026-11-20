package backup

import (
	"bytes"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"snap-rq/backend/secrets"
)

func newSourceDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "app.db")+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`
		CREATE TABLE app_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
		CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT);
		INSERT INTO notes (body) VALUES ('the-quick-brown-fox'), ('jumps-over');`); err != nil {
		t.Fatal(err)
	}
	return db
}

func notes(t *testing.T, path string) string {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var got string
	if err := db.QueryRow(`SELECT group_concat(body, ',') FROM notes`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestBackupRoundTripsAndHidesTheData(t *testing.T) {
	secrets.SetDefault(secrets.NewVault(secrets.MemoryKeyStore()))
	db := newSourceDB(t)
	dir := t.TempDir()

	info, err := Create(db, Options{Dir: dir, Kind: Automatic, Reason: `before Delete collection "A/B"`, AppVersion: "1.2.3"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(info.Path)
	if bytes.Contains(raw, []byte("the-quick-brown-fox")) {
		t.Error("the backup file holds the data in plain text")
	}
	if info.Header.Reason != `before Delete collection "A/B"` || info.Header.KeyFingerprint == "" {
		t.Errorf("header = %+v", info.Header)
	}

	dest := filepath.Join(t.TempDir(), "restored.db")
	if _, err := Extract(info.Path, dest, db, Unlock{}); err != nil {
		t.Fatal(err)
	}
	if got := notes(t, dest); got != "the-quick-brown-fox,jumps-over" {
		t.Errorf("restored notes = %q", got)
	}
}

func TestBackupWithAnotherKeyNeedsTheRecoveryKeyOrPassword(t *testing.T) {
	original := secrets.NewVault(secrets.MemoryKeyStore())
	secrets.SetDefault(original)
	db := newSourceDB(t)
	info, err := Create(db, Options{Dir: t.TempDir(), Kind: Manual, Password: "correct horse"})
	if err != nil {
		t.Fatal(err)
	}
	recovery, err := original.RecoveryKey(db)
	if err != nil {
		t.Fatal(err)
	}

	// A new computer: a different key, and an empty database.
	secrets.SetDefault(secrets.NewVault(secrets.MemoryKeyStore()))
	other := newSourceDB(t)
	dest := filepath.Join(t.TempDir(), "restored.db")

	if _, err := Extract(info.Path, dest, other, Unlock{}); !errors.Is(err, ErrNeedsKey) {
		t.Errorf("without a key: %v, want ErrNeedsKey", err)
	}
	if _, err := Extract(info.Path, dest, other, Unlock{Password: "wrong"}); !errors.Is(err, ErrWrongUnlock) {
		t.Errorf("wrong password: %v, want ErrWrongUnlock", err)
	}
	if _, err := Extract(info.Path, dest, other, Unlock{RecoveryKey: recovery}); err != nil {
		t.Fatalf("with the recovery key: %v", err)
	}
	os.Remove(dest)
	if _, err := Extract(info.Path, dest, other, Unlock{Password: "correct horse"}); err != nil {
		t.Fatalf("with the password: %v", err)
	}
	if got := notes(t, dest); got != "the-quick-brown-fox,jumps-over" {
		t.Errorf("restored notes = %q", got)
	}
}

func TestAChangedBackupIsRejected(t *testing.T) {
	secrets.SetDefault(secrets.NewVault(secrets.MemoryKeyStore()))
	db := newSourceDB(t)
	info, err := Create(db, Options{Dir: t.TempDir(), Kind: Manual, Reason: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(info.Path)
	// Change the reason in the header without breaking its JSON.
	changed := bytes.Replace(raw, []byte(`"reason":"manual"`), []byte(`"reason":"MANUAL"`), 1)
	os.WriteFile(info.Path, changed, 0o600)

	if _, err := Extract(info.Path, filepath.Join(t.TempDir(), "x.db"), db, Unlock{}); err == nil {
		t.Error("a backup with a changed header was accepted")
	}
}

func TestListIsNewestFirstAndCleansUpLeftovers(t *testing.T) {
	secrets.SetDefault(secrets.NewVault(secrets.MemoryKeyStore()))
	db := newSourceDB(t)
	dir := t.TempDir()
	first, _ := Create(db, Options{Dir: dir, Kind: Manual, Reason: "first"})
	time.Sleep(1100 * time.Millisecond) // CreatedAt has second precision
	second, _ := Create(db, Options{Dir: dir, Kind: Manual, Reason: "second"})

	leftover := filepath.Join(dir, tempPrefix+"abc.db")
	os.WriteFile(leftover, []byte("x"), 0o600)
	old := time.Now().Add(-2 * time.Hour)
	os.Chtimes(leftover, old, old)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a backup"), 0o600)

	list, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Path != second.Path || list[1].Path != first.Path {
		t.Fatalf("list = %+v, want second then first", list)
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Error("an old leftover from an interrupted backup was not removed")
	}
}

func TestFileNamesAreSafe(t *testing.T) {
	h := Header{CreatedAt: time.Date(2026, 11, 20, 15, 30, 5, 0, time.Local), Reason: `before Delete collection a/b:c*"d"`}
	name := fileName(h)
	if name != "2026-11-20 15-30-05 – before Delete collection a b c d"+Extension {
		t.Errorf("file name = %q", name)
	}
}
