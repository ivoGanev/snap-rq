package requests

import (
	"database/sql"
	"strings"
	"testing"

	"snap-rq/backend/models"
	"snap-rq/backend/secrets"

	_ "modernc.org/sqlite"
)

func newSecretsTestDB(t *testing.T, vault *secrets.Vault, vars map[string]string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`
		CREATE TABLE app_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
		CREATE TABLE environment_variables (
			id INTEGER PRIMARY KEY, environment_id INTEGER, key TEXT, value TEXT, secret INTEGER);
	`); err != nil {
		t.Fatal(err)
	}
	for key, plain := range vars {
		encrypted, err := vault.Encrypt(db, plain)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO environment_variables (environment_id, key, value, secret)
			VALUES (1, ?, ?, 1)`, key, encrypted); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestSendDecryptsOnlyTheSecretsTheRequestUses(t *testing.T) {
	vault := secrets.NewVault(secrets.MemoryKeyStore())
	secrets.SetDefault(vault)
	db := newSecretsTestDB(t, vault, map[string]string{"API_KEY": "s3cr3t", "OTHER": "unused"})

	got, err := Send{db: db}.secretsFor(1, models.HttpRequest{
		URL:            "https://api.example.com/items",
		RequestHeaders: `[{"key":"Authorization","value":"Bearer {{ API_KEY }}"}]`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["API_KEY"] != "s3cr3t" {
		t.Errorf("API_KEY = %q, want the decrypted value", got["API_KEY"])
	}
	if _, ok := got["OTHER"]; ok {
		t.Error("OTHER was decrypted although the request doesn't use it")
	}
}

func TestSendFailsClearlyWhenASecretIsLocked(t *testing.T) {
	db := newSecretsTestDB(t, secrets.NewVault(secrets.MemoryKeyStore()), map[string]string{"API_KEY": "s3cr3t"})
	secrets.SetDefault(secrets.NewVault(secrets.MemoryKeyStore())) // the key is gone

	_, err := Send{db: db}.secretsFor(1, models.HttpRequest{URL: "https://x/{{API_KEY}}"})
	if err == nil || !strings.Contains(err.Error(), "API_KEY is locked") {
		t.Errorf("error = %v, want one naming the locked variable", err)
	}
}

func TestLocalErrorsDoNotKeepSecretValues(t *testing.T) {
	msg := `Get "https://api.example.com/?key=s3cr3t": dial tcp: no such host`
	got := hideSecrets(msg, map[string]string{"API_KEY": "s3cr3t"})
	if strings.Contains(got, "s3cr3t") || !strings.Contains(got, secretMask) {
		t.Errorf("hideSecrets = %q", got)
	}
}
