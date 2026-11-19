package mcp

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1) // one in-memory database for every statement
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE app_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	return NewStore(db)
}

func TestStoreDefaults(t *testing.T) {
	cfg := newTestStore(t).Load()
	if cfg.Enabled || cfg.Port != DefaultPort || len(cfg.Overrides) != 0 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	s := newTestStore(t)
	if err := s.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPort(50000); err != nil {
		t.Fatal(err)
	}
	if err := s.SetToolOverride("list_scripts", false); err != nil {
		t.Fatal(err)
	}
	cfg := s.Load()
	if !cfg.Enabled || cfg.Port != 50000 || cfg.Overrides["list_scripts"] != false {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if _, present := cfg.Overrides["list_scripts"]; !present {
		t.Fatal("override not stored")
	}
	if err := s.ClearToolOverride("list_scripts"); err != nil {
		t.Fatal(err)
	}
	if _, present := s.Load().Overrides["list_scripts"]; present {
		t.Fatal("override not cleared")
	}
}

func TestStoreRejectsBadInput(t *testing.T) {
	s := newTestStore(t)
	if s.SetPort(80) == nil || s.SetPort(70000) == nil {
		t.Error("ports outside 1024-65535 must be rejected")
	}
	if s.SetToolOverride("not_a_tool", true) == nil {
		t.Error("unknown tools must be rejected")
	}
}

func TestTokenIsStableUntilRotated(t *testing.T) {
	s := newTestStore(t)
	if s.Token() != "" {
		t.Fatal("no token before first use")
	}
	first, err := s.EnsureToken()
	if err != nil || first == "" {
		t.Fatalf("EnsureToken: %q %v", first, err)
	}
	again, _ := s.EnsureToken()
	if again != first {
		t.Fatal("EnsureToken must keep the existing token")
	}
	rotated, _ := s.RotateToken()
	if rotated == first || s.Token() != rotated {
		t.Fatal("RotateToken must replace the token")
	}
}
