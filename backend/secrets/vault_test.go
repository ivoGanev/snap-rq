package secrets

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func newDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE app_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestEncryptCreatesKeyOnFirstUseAndRoundTrips(t *testing.T) {
	db := newDB(t)
	store := MemoryKeyStore()
	v := NewVault(store)

	if state, _ := v.Status(db); state != StateNone {
		t.Fatalf("status before first secret = %q, want none", state)
	}
	stored, err := v.Encrypt(db, "s3cr3t")
	if err != nil {
		t.Fatal(err)
	}
	if !IsEncrypted(stored) || strings.Contains(stored, "s3cr3t") {
		t.Fatalf("stored value %q is not encrypted", stored)
	}
	if again, _ := v.Encrypt(db, "s3cr3t"); again == stored {
		t.Error("encrypting twice gave the same text; the nonce must differ")
	}
	if _, err := store.Load(); err != nil {
		t.Errorf("key was not saved to the store: %v", err)
	}

	// A fresh vault (next session) reads the key back from the store.
	plain, err := NewVault(store).Decrypt(db, stored)
	if err != nil || plain != "s3cr3t" {
		t.Fatalf("decrypt = %q, %v; want s3cr3t", plain, err)
	}
	if state, _ := NewVault(store).Status(db); state != StateUnlocked {
		t.Errorf("status = %q, want unlocked", state)
	}
}

func TestMissingKeyLocksAndNeverCreatesANewOne(t *testing.T) {
	db := newDB(t)
	stored, err := NewVault(MemoryKeyStore()).Encrypt(db, "s3cr3t")
	if err != nil {
		t.Fatal(err)
	}

	emptyStore := MemoryKeyStore() // the keyring entry is gone
	v := NewVault(emptyStore)
	if state, _ := v.Status(db); state != StateLocked {
		t.Errorf("status = %q, want locked", state)
	}
	if _, err := v.Decrypt(db, stored); !errors.Is(err, ErrLocked) {
		t.Errorf("decrypt error = %v, want ErrLocked", err)
	}
	if _, err := v.Encrypt(db, "new"); !errors.Is(err, ErrLocked) {
		t.Errorf("encrypt error = %v, want ErrLocked (no new key while secrets exist)", err)
	}
	if _, err := emptyStore.Load(); !errors.Is(err, ErrNoKey) {
		t.Error("a new key was created while encrypted values exist")
	}
}

func TestWrongKeyLocks(t *testing.T) {
	db := newDB(t)
	if _, err := NewVault(MemoryKeyStore()).Encrypt(db, "s3cr3t"); err != nil {
		t.Fatal(err)
	}
	other := MemoryKeyStore()
	if _, err := NewVault(other).Encrypt(newDB(t), "x"); err != nil { // a key from elsewhere
		t.Fatal(err)
	}
	if state, _ := NewVault(other).Status(db); state != StateLocked {
		t.Errorf("status with another database's key = %q, want locked", state)
	}
}

func TestRecoveryKeyRestoresAccess(t *testing.T) {
	db := newDB(t)
	original := NewVault(MemoryKeyStore())
	stored, err := original.Encrypt(db, "s3cr3t")
	if err != nil {
		t.Fatal(err)
	}
	recovery, err := original.RecoveryKey(db)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(recovery, "SNRQ-") {
		t.Errorf("recovery key %q lacks the SNRQ- label", recovery)
	}

	newStore := MemoryKeyStore() // a new computer
	v := NewVault(newStore)
	if err := v.Restore(db, "snrq-0000-0000"); err == nil {
		t.Error("a malformed recovery key was accepted")
	}
	wrong, _ := NewVault(MemoryKeyStore()).RecoveryKey(newDB(t))
	if wrong != "" {
		if err := v.Restore(db, wrong); err == nil {
			t.Error("another database's recovery key was accepted")
		}
	}
	// Typed sloppily: lower case, spaces instead of dashes.
	sloppy := strings.ToLower(strings.ReplaceAll(recovery, "-", " "))
	if err := v.Restore(db, sloppy); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if plain, err := v.Decrypt(db, stored); err != nil || plain != "s3cr3t" {
		t.Errorf("after restore decrypt = %q, %v", plain, err)
	}
	if _, err := newStore.Load(); err != nil {
		t.Error("restore did not save the key to the store")
	}
}

func TestForgetLetsALockedDatabaseStartOver(t *testing.T) {
	db := newDB(t)
	if _, err := NewVault(MemoryKeyStore()).Encrypt(db, "s3cr3t"); err != nil {
		t.Fatal(err)
	}
	v := NewVault(MemoryKeyStore())
	if err := v.Forget(db); err != nil {
		t.Fatal(err)
	}
	if state, _ := v.Status(db); state != StateNone {
		t.Errorf("status after forget = %q, want none", state)
	}
	stored, err := v.Encrypt(db, "fresh")
	if err != nil {
		t.Fatalf("encrypt after forget: %v", err)
	}
	if plain, _ := v.Decrypt(db, stored); plain != "fresh" {
		t.Errorf("decrypt after forget = %q", plain)
	}
}

func TestFileKeyStoreIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "secret.key")
	s := fileKeyStore{path: path}
	if _, err := s.Load(); !errors.Is(err, ErrNoKey) {
		t.Fatalf("empty file store: %v, want ErrNoKey", err)
	}
	key, _ := newKey()
	if err := s.Save(key); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load()
	if err != nil || string(got) != string(key) {
		t.Fatalf("load = %v, %v", got, err)
	}
}

func TestARolledBackFirstEncryptDoesNotLoseTheKeyCheck(t *testing.T) {
	db := newDB(t)
	v := NewVault(MemoryKeyStore())

	tx, _ := db.Begin()
	if _, err := v.Encrypt(tx, "lost"); err != nil {
		t.Fatal(err)
	}
	tx.Rollback() // the command failed after encrypting: the key check is gone too

	stored, err := v.Encrypt(db, "kept")
	if err != nil {
		t.Fatal(err)
	}
	if state, _ := NewVault(v.store).Status(db); state != StateUnlocked {
		t.Fatalf("status = %q, want unlocked: the key check must be written again", state)
	}
	if plain, _ := NewVault(v.store).Decrypt(db, stored); plain != "kept" {
		t.Errorf("decrypt in a new session = %q", plain)
	}
}

func TestReadsNeverCreateAKey(t *testing.T) {
	db := newDB(t)
	store := MemoryKeyStore()
	v := NewVault(store)
	if _, err := v.RecoveryKey(db); !errors.Is(err, ErrNoSecrets) {
		t.Errorf("recovery key before any secret: %v, want ErrNoSecrets", err)
	}
	if _, err := store.Load(); !errors.Is(err, ErrNoKey) {
		t.Error("a read created a key")
	}
}
