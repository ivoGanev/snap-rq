package repository_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"snap-rq/backend/database"
	"snap-rq/backend/models"
	"snap-rq/backend/repository"
	"snap-rq/backend/secrets"
)

type fixture struct {
	t         *testing.T
	db        *sql.DB
	vault     *secrets.Vault
	projectID int64
}

// newFixture opens a fresh database (full schema and migrations) with the
// default project, and a vault that can open what it encrypts.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	db, err := database.OpenPath(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	vault := secrets.NewVault(secrets.MemoryKeyStore())
	secrets.SetDefault(vault)
	f := &fixture{t: t, db: db, vault: vault}
	if err := db.QueryRow(`SELECT id FROM projects ORDER BY id LIMIT 1`).Scan(&f.projectID); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) exec(query string, args ...any) int64 {
	f.t.Helper()
	res, err := f.db.Exec(query, args...)
	if err != nil {
		f.t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func (f *fixture) environment(projectID int64, name string) int64 {
	return f.exec(`INSERT INTO environments (project_id, name) VALUES (?, ?)`, projectID, name)
}

func (f *fixture) plain(envID int64, key, value string) {
	f.exec(`INSERT INTO environment_variables (environment_id, key, value, secret) VALUES (?, ?, ?, 0)`, envID, key, value)
}

func (f *fixture) secret(envID int64, key, value string) string {
	f.t.Helper()
	encrypted, err := f.vault.Encrypt(f.db, value)
	if err != nil {
		f.t.Fatal(err)
	}
	f.exec(`INSERT INTO environment_variables (environment_id, key, value, secret) VALUES (?, ?, ?, 1)`, envID, key, encrypted)
	return encrypted
}

func values(env models.Environment) map[string]string {
	out := map[string]string{}
	for _, v := range env.Variables {
		out[v.Key] = v.Value
	}
	return out
}

var withVariables = repository.Load{Variables: true, Secrets: repository.Decrypt}

func TestGetLoadsVariablesOnlyWhenAsked(t *testing.T) {
	f := newFixture(t)
	envID := f.environment(f.projectID, "Staging")
	f.plain(envID, "HOST", "eu")
	envs := repository.NewEnvironments(f.db)

	bare, err := envs.Get(envID, repository.Load{})
	if err != nil || bare.Name != "Staging" || bare.ProjectID != f.projectID || bare.Variables != nil {
		t.Errorf("without variables: %+v (%v)", bare, err)
	}
	full, err := envs.Get(envID, withVariables)
	if err != nil || values(full)["HOST"] != "eu" || !full.Variables[0].HasValue {
		t.Errorf("with variables: %+v (%v)", full, err)
	}
}

func TestGetDecryptsOrKeepsSecretsAsAsked(t *testing.T) {
	f := newFixture(t)
	envID := f.environment(f.projectID, "Prod")
	encrypted := f.secret(envID, "TOKEN", "s3cr3t")
	envs := repository.NewEnvironments(f.db)

	opened, err := envs.Get(envID, withVariables)
	if err != nil || values(opened)["TOKEN"] != "s3cr3t" || opened.Variables[0].Locked {
		t.Errorf("decrypted: %+v (%v)", opened.Variables, err)
	}
	kept, err := envs.Get(envID, repository.Load{Variables: true, Secrets: repository.KeepEncrypted})
	if err != nil || values(kept)["TOKEN"] != encrypted {
		t.Errorf("kept encrypted: %+v (%v)", kept.Variables, err)
	}
}

func TestALockedVaultMarksSecretsLockedAndStillLoads(t *testing.T) {
	f := newFixture(t)
	envID := f.environment(f.projectID, "Prod")
	f.secret(envID, "A", "one")
	f.secret(envID, "B", "two")
	f.plain(envID, "HOST", "eu")
	secrets.SetDefault(secrets.NewVault(secrets.MemoryKeyStore())) // the key is gone

	env, err := repository.NewEnvironments(f.db).Get(envID, withVariables)
	if err != nil {
		t.Fatalf("a locked vault failed the load: %v", err)
	}
	for _, v := range env.Variables {
		if v.Secret && (!v.Locked || v.Value != "") {
			t.Errorf("%s = %+v, want locked with no value", v.Key, v)
		}
		if !v.Secret && v.Value != "eu" {
			t.Errorf("the plain variable changed: %+v", v)
		}
	}
}

func TestOneDamagedSecretOnlyLocksItself(t *testing.T) {
	f := newFixture(t)
	envID := f.environment(f.projectID, "Prod")
	f.secret(envID, "GOOD", "fine")
	f.exec(`INSERT INTO environment_variables (environment_id, key, value, secret) VALUES (?, 'BAD', 'not-a-ciphertext', 1)`, envID)
	f.secret(envID, "ALSO_GOOD", "fine too")

	env, err := repository.NewEnvironments(f.db).Get(envID, withVariables)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range env.Variables {
		if (v.Key == "BAD") != v.Locked {
			t.Errorf("%s locked = %v", v.Key, v.Locked)
		}
	}
	if values(env)["ALSO_GOOD"] != "fine too" {
		t.Errorf("a secret after the damaged one wasn't opened: %v", values(env))
	}
}

func TestNoEnvironmentIsEmptyAndAnUnknownOneIsAnError(t *testing.T) {
	f := newFixture(t)
	envs := repository.NewEnvironments(f.db)
	if env, err := envs.Get(0, withVariables); err != nil || env.ID != 0 || len(env.Variables) != 0 {
		t.Errorf("environment 0 = %+v (%v), want empty", env, err)
	}
	if _, err := envs.Get(9999, withVariables); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("unknown environment: %v", err)
	}
}

func TestBatchLoadsPutEachVariableOnItsEnvironment(t *testing.T) {
	f := newFixture(t)
	other := f.exec(`INSERT INTO projects (profile_id, name) SELECT profile_id, 'Other' FROM projects WHERE id = ?`, f.projectID)
	a := f.environment(f.projectID, "A")
	b := f.environment(f.projectID, "B")
	c := f.environment(other, "C")
	f.plain(a, "K", "a")
	f.plain(b, "K", "b")
	f.plain(c, "K", "c")
	f.secret(b, "S", "b-secret")
	envs := repository.NewEnvironments(f.db)

	many, err := envs.GetMany([]int64{c, a}, withVariables)
	if err != nil || len(many) != 2 || many[0].ID != a || many[1].ID != c ||
		values(many[0])["K"] != "a" || values(many[1])["K"] != "c" {
		t.Errorf("GetMany = %+v (%v)", many, err)
	}

	listed, err := envs.ListForProject(f.projectID, withVariables)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]map[string]string{}
	for _, e := range listed {
		got[e.Name] = values(e)
	}
	if got["A"]["K"] != "a" || got["B"]["K"] != "b" || got["B"]["S"] != "b-secret" || got["C"] != nil {
		t.Errorf("ListForProject = %v", got)
	}
	if empty, err := envs.GetMany(nil, withVariables); err != nil || empty != nil {
		t.Errorf("GetMany(nil) = %v, %v", empty, err)
	}
}

func TestCurrentIsTheProjectsStoredEnvironment(t *testing.T) {
	f := newFixture(t)
	envID := f.environment(f.projectID, "Staging")
	f.plain(envID, "HOST", "eu")
	envs := repository.NewEnvironments(f.db)

	if env, err := envs.Current(f.projectID, withVariables); err != nil || env.ID != 0 {
		t.Errorf("nothing stored = %+v (%v), want none", env, err)
	}
	f.exec(`UPDATE projects SET selected_environment_id = ? WHERE id = ?`, envID, f.projectID)
	if env, err := envs.Current(f.projectID, withVariables); err != nil || env.ID != envID || values(env)["HOST"] != "eu" {
		t.Errorf("stored = %+v (%v)", env, err)
	}
	if env, err := envs.Current(9999, withVariables); err != nil || env.ID != 0 {
		t.Errorf("unknown project = %+v (%v), want none", env, err)
	}
}

func TestDeletingTheSelectedEnvironmentClearsTheSelection(t *testing.T) {
	f := newFixture(t)
	envID := f.environment(f.projectID, "Staging")
	f.exec(`UPDATE projects SET selected_environment_id = ? WHERE id = ?`, envID, f.projectID)

	f.exec(`DELETE FROM environments WHERE id = ?`, envID)

	var selected sql.NullInt64
	if err := f.db.QueryRow(`SELECT selected_environment_id FROM projects WHERE id = ?`, f.projectID).Scan(&selected); err != nil {
		t.Fatal(err)
	}
	if selected.Valid {
		t.Errorf("selected_environment_id = %d after its environment was deleted, want NULL", selected.Int64)
	}
}

func TestRequestsGetLoadsTheWholeRequest(t *testing.T) {
	f := newFixture(t)
	var collectionID int64
	if err := f.db.QueryRow(`SELECT id FROM collections WHERE project_id = ? LIMIT 1`, f.projectID).Scan(&collectionID); err != nil {
		t.Fatal(err)
	}
	id := f.exec(`INSERT INTO http_requests (collection_id, name, url, method, body, body_type, body_form, request_headers)
		VALUES (?, 'Get item', 'https://x/{{ID}}', 'GET', '{"a":1}', 'json', '[]', 'X-One: 1')`, collectionID)

	req, err := repository.NewRequests(f.db).Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if req.ID != id || req.CollectionID != collectionID || req.ProjectID != f.projectID || req.Name != "Get item" ||
		req.URL != "https://x/{{ID}}" || req.Body != `{"a":1}` || req.BodyForm != "[]" || req.RequestHeaders != "X-One: 1" {
		t.Errorf("request = %+v", req)
	}
	if _, err := repository.NewRequests(f.db).Get(9999); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("unknown request: %v", err)
	}
}
