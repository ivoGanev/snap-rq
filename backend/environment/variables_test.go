package environment

import (
	"database/sql"
	"strings"
	"testing"

	"snap-rq/backend/models"
	"snap-rq/backend/secrets"

	_ "modernc.org/sqlite"
)

// testDB holds environment 1 with the given plain and secret variables,
// the secrets encrypted with vault.
func testDB(t *testing.T, vault *secrets.Vault, plain, secret map[string]string) *sql.DB {
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
	insert := func(key, value string, secret bool) {
		if _, err := db.Exec(`INSERT INTO environment_variables (environment_id, key, value, secret)
			VALUES (1, ?, ?, ?)`, key, value, secret); err != nil {
			t.Fatal(err)
		}
	}
	for key, value := range plain {
		insert(key, value, false)
	}
	for key, value := range secret {
		encrypted, err := vault.Encrypt(db, value)
		if err != nil {
			t.Fatal(err)
		}
		insert(key, encrypted, true)
	}
	return db
}

func load(t *testing.T, db *sql.DB, environmentID int64) *Variables {
	t.Helper()
	v, err := Load(db, environmentID)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestFillHandlesEveryAllowedNameWithPadding(t *testing.T) {
	db := testDB(t, secrets.NewVault(secrets.MemoryKeyStore()),
		map[string]string{"fleet-management": "fm", "api.v2": "v2", "base_url": "http://x"}, nil)
	got, locked := load(t, db, 1).Fill("{{base_url}}/{{ fleet-management }}/{{api.v2}}/{{missing}}")
	if want := "http://x/fm/v2/{{missing}}"; got != want || len(locked) != 0 {
		t.Errorf("got %q (locked %v), want %q", got, locked, want)
	}
}

func TestOnlyTheSecretsUsedAreDecrypted(t *testing.T) {
	vault := secrets.NewVault(secrets.MemoryKeyStore())
	secrets.SetDefault(vault)
	db := testDB(t, vault, nil, map[string]string{"API_KEY": "s3cr3t", "OTHER": "unused"})
	v := load(t, db, 1)

	req, locked := v.FillRequest(models.HttpRequest{
		URL: "https://api.example.com/items", RequestHeaders: "Authorization: Bearer {{ API_KEY }}",
	})

	if req.RequestHeaders != "Authorization: Bearer s3cr3t" || len(locked) != 0 {
		t.Errorf("headers = %q (locked %v), want the decrypted value", req.RequestHeaders, locked)
	}
	if _, ok := v.Revealed()["OTHER"]; ok || v.Revealed()["API_KEY"] != "s3cr3t" {
		t.Errorf("revealed = %v, want only API_KEY", v.Revealed())
	}
}

func TestALockedSecretStaysAPlaceholderAndIsReported(t *testing.T) {
	db := testDB(t, secrets.NewVault(secrets.MemoryKeyStore()), map[string]string{"HOST": "eu"},
		map[string]string{"API_KEY": "s3cr3t"})
	secrets.SetDefault(secrets.NewVault(secrets.MemoryKeyStore())) // the key is gone
	v := load(t, db, 1)

	got, locked := v.Fill("https://{{HOST}}.example.com/{{API_KEY}}")
	if got != "https://eu.example.com/{{API_KEY}}" || len(locked) != 1 || locked[0] != "API_KEY" {
		t.Errorf("got %q, locked %v", got, locked)
	}
	if _, ok := v.Get("API_KEY"); ok {
		t.Error("a locked secret reads as set")
	}
}

func TestFormDataTextIsFilledButNotFilePaths(t *testing.T) {
	db := testDB(t, secrets.NewVault(secrets.MemoryKeyStore()), map[string]string{"NAME": "Ada"}, nil)
	req, _ := load(t, db, 1).FillRequest(models.HttpRequest{BodyType: "form-data", BodyForm: `[
		{"key":"title","type":"text","value":"Hi {{NAME}}","enabled":true},
		{"key":"doc","type":"file","file_path":"/files/{{NAME}}.txt","enabled":true}]`})
	if !strings.Contains(req.BodyForm, `"value":"Hi Ada"`) || !strings.Contains(req.BodyForm, `/files/{{NAME}}.txt`) {
		t.Errorf("body_form = %s", req.BodyForm)
	}
}

func TestNoEnvironmentFillsNothing(t *testing.T) {
	db := testDB(t, secrets.NewVault(secrets.MemoryKeyStore()), map[string]string{"NAME": "Ada"}, nil)
	if got, _ := load(t, db, 0).Fill("{{NAME}}"); got != "{{NAME}}" {
		t.Errorf("got %q", got)
	}
}
