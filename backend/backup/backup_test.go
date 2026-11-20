package backup

import (
	"bytes"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestPruneKeepsTheNewestAutomaticAndEveryManualBackup(t *testing.T) {
	secrets.SetDefault(secrets.NewVault(secrets.MemoryKeyStore()))
	db := newSourceDB(t)
	dir := t.TempDir()
	manual, _ := Create(db, Options{Dir: dir, Kind: Manual, Reason: "manual"})
	var autos []Info
	for i := 0; i < 3; i++ {
		info, _ := Create(db, Options{Dir: dir, Kind: Automatic, Reason: "auto"})
		autos = append(autos, info)
		time.Sleep(1100 * time.Millisecond)
	}

	Prune(dir, 2, 24*time.Hour)

	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	if !exists(manual.Path) {
		t.Error("a manual backup was pruned")
	}
	if exists(autos[0].Path) || !exists(autos[1].Path) || !exists(autos[2].Path) {
		t.Error("prune should drop only the oldest automatic backup")
	}
}

func TestRestoreSwapsTheBackupInOnTheNextStart(t *testing.T) {
	secrets.SetDefault(secrets.NewVault(secrets.MemoryKeyStore()))
	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "app.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(`CREATE TABLE app_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
		CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT);
		INSERT INTO notes (body) VALUES ('original');`)
	folder := t.TempDir()
	s := DefaultSettings()
	s.Folder = folder
	SaveSettings(db, s)

	saved, err := Take(db, Manual, "manual")
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(`UPDATE notes SET body = 'changed later'`)

	if _, err := PrepareRestore(db, dataDir, saved.Path, Unlock{}); err != nil {
		t.Fatal(err)
	}
	list, _ := List(folder)
	if len(list) != 2 || !strings.HasPrefix(list[0].Header.Reason, "before restore of") {
		t.Fatalf("want a 'before restore' backup next to the original: %+v", list)
	}
	db.Close() // the app restarts

	name, err := ApplyPendingRestore(dataDir, dbPath)
	if err != nil || name != filepath.Base(saved.Path) {
		t.Fatalf("apply = %q, %v", name, err)
	}
	if got := notes(t, dbPath); got != "original" {
		t.Errorf("after restore notes = %q, want original", got)
	}
	if again, _ := ApplyPendingRestore(dataDir, dbPath); again != "" {
		t.Error("a restore was applied twice")
	}
}

func TestBackupsFromANewerVersionAreRefused(t *testing.T) {
	secrets.SetDefault(secrets.NewVault(secrets.MemoryKeyStore()))
	db := newSourceDB(t)
	info, err := Create(db, Options{Dir: t.TempDir(), Kind: Manual, AppVersion: "9.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	old := AppVersion
	AppVersion = "1.0.0"
	defer func() { AppVersion = old }()
	if _, err := PrepareRestore(db, t.TempDir(), info.Path, Unlock{}); !errors.Is(err, ErrNewerBackup) {
		t.Errorf("err = %v, want ErrNewerBackup", err)
	}
}

func TestNewerVersion(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"1.2.0", "1.1.9", true}, {"v2.0.0", "1.9.9", true}, {"1.0.0", "1.0.0", false},
		{"1.0.0", "1.0.1", false}, {"0.0.0-dev", "1.0.0", false}, {"2.0.0", "0.0.0-dev", false},
	} {
		if got := newerVersion(c.a, c.b); got != c.want {
			t.Errorf("newerVersion(%q, %q) = %v", c.a, c.b, got)
		}
	}
}
