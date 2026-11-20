package variables_test

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"snap-rq/backend/commands"
	"snap-rq/backend/commands/catalog"
	"snap-rq/backend/database"
	"snap-rq/backend/models"
	"snap-rq/backend/secrets"
)

type fixture struct {
	t      *testing.T
	db     *sql.DB
	runner *commands.Runner
	envID  int64
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	secrets.SetDefault(secrets.NewVault(secrets.MemoryKeyStore()))
	db, err := database.OpenPath(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("opening database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	runner := commands.NewRunner(db)
	if err := runner.Register(catalog.All...); err != nil {
		t.Fatalf("registering commands: %v", err)
	}
	res, err := db.Exec(`INSERT INTO environments (project_id, name) SELECT id, 'Test' FROM projects LIMIT 1`)
	if err != nil {
		t.Fatalf("creating an environment: %v", err)
	}
	envID, _ := res.LastInsertId()
	return fixture{t: t, db: db, runner: runner, envID: envID}
}

func (f fixture) run(command string, in any) any {
	f.t.Helper()
	raw, _ := json.Marshal(in)
	out, err := f.runner.Run(command, raw)
	if err != nil {
		f.t.Fatalf("%s: %v", command, err)
	}
	return out
}

func (f fixture) stored(id int64) (value string, secret bool) {
	f.t.Helper()
	if err := f.db.QueryRow(`SELECT value, secret FROM environment_variables WHERE id = ?`, id).
		Scan(&value, &secret); err != nil {
		f.t.Fatal(err)
	}
	return value, secret
}

// createSecret creates a secret the way the editor does: empty, then its value.
func (f fixture) createSecret(key, value string) models.EnvironmentVariable {
	f.t.Helper()
	created := f.run("variable.create", map[string]any{
		"environment_id": f.envID, "key": key, "secret": true,
	}).(models.EnvironmentVariable)
	return f.run("variable.update", map[string]any{"id": created.ID, "key": key, "value": value}).(models.EnvironmentVariable)
}

func TestSecretIsStoredEncryptedAndNeverListed(t *testing.T) {
	f := newFixture(t)
	created := f.createSecret("API_KEY", "s3cr3t")
	if created.Value != "" || !created.HasValue || !created.Secret {
		t.Errorf("create returned %+v; want no value, has_value, secret", created)
	}
	if value, _ := f.stored(created.ID); !secrets.IsEncrypted(value) || strings.Contains(value, "s3cr3t") {
		t.Errorf("stored value %q is not encrypted", value)
	}

	listed := f.run("variable.list", map[string]any{"environment_id": f.envID}).([]models.EnvironmentVariable)
	for _, v := range listed {
		if v.ID == created.ID && (v.Value != "" || !v.HasValue) {
			t.Errorf("list returned %+v; a secret's value must not be listed", v)
		}
	}
	if got := f.run("variable.reveal", map[string]any{"id": created.ID}); got != "s3cr3t" {
		t.Errorf("reveal = %v, want s3cr3t", got)
	}
}

func TestUpdateKeepsASecretUnlessANewValueIsGiven(t *testing.T) {
	f := newFixture(t)
	created := f.createSecret("API_KEY", "s3cr3t")

	f.run("variable.update", map[string]any{"id": created.ID, "key": "TOKEN"}) // rename only
	if got := f.run("variable.reveal", map[string]any{"id": created.ID}); got != "s3cr3t" {
		t.Errorf("after renaming, reveal = %v; the value must be kept", got)
	}

	f.run("variable.update", map[string]any{"id": created.ID, "key": "TOKEN", "value": "n3w"})
	if value, _ := f.stored(created.ID); !secrets.IsEncrypted(value) {
		t.Errorf("new secret value stored as %q, want encrypted", value)
	}
	if got := f.run("variable.reveal", map[string]any{"id": created.ID}); got != "n3w" {
		t.Errorf("reveal = %v, want n3w", got)
	}
}

func TestSetSecretEncryptsAndDecryptsInPlace(t *testing.T) {
	f := newFixture(t)
	plain := f.run("variable.create", map[string]any{
		"environment_id": f.envID, "key": "API_KEY", "value": "s3cr3t",
	}).(models.EnvironmentVariable)

	f.run("variable.set_secret", map[string]any{"id": plain.ID, "secret": true})
	if value, secret := f.stored(plain.ID); !secret || !secrets.IsEncrypted(value) {
		t.Errorf("after set_secret(true) stored %q secret=%v", value, secret)
	}

	f.run("variable.set_secret", map[string]any{"id": plain.ID, "secret": false})
	if value, secret := f.stored(plain.ID); secret || value != "s3cr3t" {
		t.Errorf("after set_secret(false) stored %q secret=%v, want the plain value", value, secret)
	}
}

func TestASecretCannotBeCreatedWithItsValue(t *testing.T) {
	f := newFixture(t)
	raw, _ := json.Marshal(map[string]any{"environment_id": f.envID, "key": "API_KEY", "value": "s3cr3t", "secret": true})
	if _, err := f.runner.Run("variable.create", raw); err == nil {
		t.Error("creating a secret with its value must fail: create's input goes into the undo log")
	}
}

func TestUndoingADeleteKeepsTheSecretEncrypted(t *testing.T) {
	f := newFixture(t)
	created := f.createSecret("API_KEY", "s3cr3t")
	f.run("variable.delete", map[string]any{"id": created.ID})

	var logged int
	f.db.QueryRow(`SELECT COUNT(*) FROM undo_log
		WHERE input LIKE '%s3cr3t%' OR snapshot LIKE '%s3cr3t%' OR result LIKE '%s3cr3t%'`).Scan(&logged)
	if logged != 0 {
		t.Errorf("the undo log holds the plain secret in %d rows", logged)
	}

	if _, err := f.runner.Undo(); err != nil {
		t.Fatal(err)
	}
	if value, secret := f.stored(created.ID); !secret || !secrets.IsEncrypted(value) {
		t.Errorf("after undo stored %q secret=%v, want it back as an encrypted secret", value, secret)
	}
	if got := f.run("variable.reveal", map[string]any{"id": created.ID}); got != "s3cr3t" {
		t.Errorf("reveal after undo = %v", got)
	}
}
