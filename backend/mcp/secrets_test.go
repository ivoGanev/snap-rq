package mcp

import (
	"strings"
	"testing"

	"snap-rq/backend/secrets"
)

func TestSecretVariableValuesAreRedactedEvenWithMaskingOff(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.db.Exec(`CREATE TABLE environment_variables (
		id INTEGER PRIMARY KEY, environment_id INTEGER, key TEXT, value TEXT, secret INTEGER)`); err != nil {
		t.Fatal(err)
	}
	vault := secrets.NewVault(secrets.MemoryKeyStore())
	secrets.SetDefault(vault)
	encrypted, err := vault.Encrypt(store.db, "tok-9f8e7d")
	if err != nil {
		t.Fatal(err)
	}
	store.db.Exec(`INSERT INTO environment_variables (environment_id, key, value, secret) VALUES (1, 'API_KEY', ?, 1)`, encrypted)

	s := &Server{store: store} // masker nil: masking turned off
	out, err := s.present(map[string]any{
		"responses": []any{map[string]any{"body": `{"echo":"tok-9f8e7d"}`, "status": 200}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := out.(map[string]any)["responses"].([]any)[0].(map[string]any)["body"].(string)
	if strings.Contains(got, "tok-9f8e7d") || !strings.Contains(got, Redacted) {
		t.Errorf("body = %q, want the secret value redacted", got)
	}
}

func TestVeryShortSecretValuesAreNotRedactedEverywhere(t *testing.T) {
	got := redactValues("status 1 of 10", []string{"1"})
	if got != "status 1 of 10" {
		t.Errorf("a one-character secret rewrote unrelated text: %q", got)
	}
}
