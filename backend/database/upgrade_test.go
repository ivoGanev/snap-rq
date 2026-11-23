package database_test

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"snap-rq/backend/backup"
	"snap-rq/backend/commands"
	"snap-rq/backend/commands/catalog"
	"snap-rq/backend/commands/requests"
	"snap-rq/backend/database"
	"snap-rq/backend/models"
	"snap-rq/backend/secrets"
)

// Each released version's database, made by that version with
// SNAPRQ_UPGRADE_FIXTURE=<version> go test ./backend/database -run TestWriteUpgradeFixture.
// They are never regenerated: they stand for what users have on disk.
const fixtureDir = "testdata/upgrade"

const (
	fixtureSecret     = "fixture-s3cr3t-token"
	fixtureScriptLine = "fixture script ran"
)

func TestEveryReleasedDatabaseUpgrades(t *testing.T) {
	fixtures, _ := filepath.Glob(filepath.Join(fixtureDir, "*.db"))
	if len(fixtures) == 0 {
		t.Fatal("no fixtures in " + fixtureDir)
	}
	for _, fixture := range fixtures {
		version := strings.TrimSuffix(filepath.Base(fixture), ".db")
		t.Run(version, func(t *testing.T) {
			path := copyFixture(t, fixture)
			useFixtureKey(t, false)

			folder := t.TempDir()
			pointBackupsAt(t, path, folder)

			db, err := database.OpenWith(path, backup.BeforeUpdate("99.0.0"))
			if err != nil {
				t.Fatalf("opening the %s database: %v", version, err)
			}
			t.Cleanup(func() { db.Close() })

			list, _ := backup.List(folder)
			if len(list) != 1 || list[0].Header.Kind != backup.Update {
				t.Errorf("backups after the upgrade = %+v, want one taken before it", list)
			}
			checkFixtureData(t, newFixtureRunner(t, db), db)
		})
	}
}

// checkFixtureData reads back, through today's commands, everything
// writeFixtureData stored.
func checkFixtureData(t *testing.T, r fixtureRunner, db *sql.DB) {
	t.Helper()
	var projectID int64
	if err := db.QueryRow(`SELECT id FROM projects ORDER BY id LIMIT 1`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}

	jsonReq := r.request(r.requestID("Create order"))
	if jsonReq.Method != "POST" || jsonReq.BodyType != "json" || !strings.Contains(jsonReq.Body, `"sku"`) ||
		!strings.Contains(jsonReq.RequestHeaders, "{{TOKEN}}") {
		t.Errorf("JSON request = %+v", jsonReq)
	}
	formReq := r.request(r.requestID("Upload avatar"))
	if formReq.BodyType != "form-data" || !strings.Contains(formReq.BodyForm, "avatar.png") {
		t.Errorf("form request = %+v", formReq)
	}

	var responses []models.HttpResponse
	r.run("response.list_for_request", map[string]any{"request_id": jsonReq.ID}, &responses)
	if len(responses) != 1 || responses[0].StatusCode != 201 || !strings.Contains(responses[0].Body, "order-1") {
		t.Fatalf("responses = %+v", responses)
	}
	if log := scriptLogText(responses[0]); !strings.Contains(log, fixtureScriptLine) || !strings.Contains(log, `"passed":true`) {
		t.Errorf("the response's script log lost the script's output: %+v", responses[0].ScriptLog)
	}

	var envs []models.Environment
	r.run("environment.list", map[string]any{"project_id": projectID}, &envs)
	if len(envs) != 1 {
		t.Fatalf("environments = %+v", envs)
	}
	var vars []models.EnvironmentVariable
	r.run("variable.list", map[string]any{"environment_id": envs[0].ID}, &vars)
	var secretID int64
	for _, v := range vars {
		if v.Key == "TOKEN" {
			secretID = v.ID
		}
	}
	if secretID == 0 {
		t.Fatalf("variables = %+v, want TOKEN", vars)
	}
	var revealed json.RawMessage
	r.run("variable.reveal", map[string]any{"id": secretID}, &revealed)
	if !strings.Contains(string(revealed), fixtureSecret) {
		t.Errorf("the secret no longer decrypts: %s", revealed)
	}

	var cookies []models.Cookie
	r.run("cookie.list", map[string]any{"project_id": projectID}, &cookies)
	if len(cookies) != 1 || cookies[0].Name != "session" {
		t.Errorf("cookies = %+v", cookies)
	}

	var favourites []models.HttpRequest
	r.run("favourite.list_requests", map[string]any{"favourite_collection_id": r.favouriteID()}, &favourites)
	if len(favourites) != 1 || favourites[0].ID != jsonReq.ID ||
		favourites[0].UserColor == nil || *favourites[0].UserColor != "secondary" {
		t.Errorf("favourite requests = %+v, want Create order in secondary", favourites)
	}

	var usage []struct {
		ID int64 `json:"id"`
	}
	r.run("request.list_usage", map[string]any{"project_id": projectID}, &usage)
	if len(usage) == 0 {
		t.Error("the usage counts are gone")
	}
}

func TestWriteUpgradeFixture(t *testing.T) {
	version := os.Getenv("SNAPRQ_UPGRADE_FIXTURE")
	if version == "" {
		t.Skip("set SNAPRQ_UPGRADE_FIXTURE=<version> to write that version's fixture")
	}
	target := filepath.Join(fixtureDir, version+".db")
	if _, err := os.Stat(target); err == nil {
		t.Fatalf("%s exists; released fixtures are never rewritten", target)
	}
	key := useFixtureKey(t, true)

	path := filepath.Join(t.TempDir(), "app.db")
	db, err := database.OpenPath(path)
	if err != nil {
		t.Fatal(err)
	}
	// What BeforeUpdate records on the release's second start.
	recorded, _ := json.Marshal(version)
	if _, err := db.Exec(`INSERT OR REPLACE INTO app_settings (key, value) VALUES ('last_run_version', ?)`,
		string(recorded)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	writeFixtureData(t, newFixtureRunner(t, db), db)
	checkFixtureData(t, newFixtureRunner(t, db), db)
	if _, err := db.Exec(`VACUUM`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	if err := os.MkdirAll(fixtureDir, 0o755); err != nil {
		t.Fatal(err)
	}
	copyFile(t, path, target)
	if committed := filepath.Join(fixtureDir, "secret.key"); !exists(committed) {
		copyFile(t, key, committed)
	}
	t.Logf("wrote %s", target)
}

// writeFixtureData stores one of everything a user keeps, the way the app
// does: through the commands, and a real send.
func writeFixtureData(t *testing.T, r fixtureRunner, db *sql.DB) {
	t.Helper()
	var projectID, collectionID int64
	if err := db.QueryRow(`SELECT id, project_id FROM collections ORDER BY id LIMIT 1`).Scan(&collectionID, &projectID); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = io.Copy(io.Discard, req.Body)
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "abc", Path: "/"})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"order-1"}`))
	}))
	defer server.Close()

	var env models.Environment
	r.run("environment.create", map[string]any{"project_id": projectID, "name": "Staging"}, &env)
	r.run("variable.create", map[string]any{"environment_id": env.ID, "key": "BASE", "value": server.URL}, nil)
	var token models.EnvironmentVariable
	r.run("variable.create", map[string]any{"environment_id": env.ID, "key": "TOKEN", "secret": true}, &token)
	r.run("variable.update", map[string]any{"id": token.ID, "key": "TOKEN", "value": fixtureSecret}, nil)

	var jsonReq models.HttpRequest
	r.run("request.create", models.HttpRequest{
		CollectionID: collectionID, Name: "Create order", Method: "POST", URL: "{{BASE}}/orders?draft=true",
		BodyType: "json", Body: `{"sku":"A-1","qty":2}`,
		RequestHeaders: "Authorization: Bearer {{TOKEN}}\nAccept: application/json",
	}, &jsonReq)

	var collection models.Collection
	r.run("collection.create", map[string]any{"project_id": projectID, "name": "Uploads"}, &collection)
	r.run("collection.set_appearance", map[string]any{
		"collection_id": collection.ID, "appearance_type": "color", "appearance_value": "tertiary",
	}, nil)
	r.run("request.create", models.HttpRequest{
		CollectionID: collection.ID, Name: "Upload avatar", Method: "PUT", URL: "{{BASE}}/avatar",
		BodyType: "form-data",
		BodyForm: `[{"key":"note","type":"text","value":"hi","enabled":true},` +
			`{"key":"file","type":"file","file_path":"/tmp/avatar.png","file_name":"avatar.png","enabled":true}]`,
	}, nil)

	r.run("script.attach", map[string]any{"request_id": jsonReq.ID, "project_id": projectID, "script_name": "Check order"}, nil)
	var scriptID int64
	if err := db.QueryRow(`SELECT id FROM scripts WHERE name = 'Check order'`).Scan(&scriptID); err != nil {
		t.Fatal(err)
	}
	r.run("script.set_content", map[string]any{"id": scriptID, "content": `function afterResponse(res, ctx) {
  console.log("` + fixtureScriptLine + `");
  ctx.test("created", () => ctx.expect(res.status).toBe(201));
}
`}, nil)

	var favourite struct {
		ID int64 `json:"id"`
	}
	r.run("favourite.create", map[string]any{"project_id": projectID, "name": "Daily"}, &favourite)
	r.run("favourite.add_request", map[string]any{"favourite_collection_id": favourite.ID, "request_id": jsonReq.ID}, nil)
	r.run("favourite.set_color", map[string]any{
		"favourite_collection_id": favourite.ID, "request_ids": []int64{jsonReq.ID}, "color": "secondary",
	}, nil)

	sent := r.request(jsonReq.ID)
	result, err := r.send(map[string]any{
		"request": sent, "environment_id": env.ID,
		"variables": []map[string]string{{"key": "BASE", "value": server.URL}},
	})
	if err != nil {
		t.Fatalf("sending: %v", err)
	}
	t.Logf("sent: %s", result)
}

type fixtureRunner struct {
	t      *testing.T
	db     *sql.DB
	runner *commands.Runner
}

func newFixtureRunner(t *testing.T, db *sql.DB) fixtureRunner {
	t.Helper()
	runner := commands.NewRunner(db)
	if err := runner.Register(catalog.All...); err != nil {
		t.Fatal(err)
	}
	return fixtureRunner{t: t, db: db, runner: runner}
}

func (r fixtureRunner) run(name string, in, out any) {
	r.t.Helper()
	raw, _ := json.Marshal(in)
	result, err := r.runner.Run(name, raw)
	if err != nil {
		r.t.Fatalf("%s: %v", name, err)
	}
	if out == nil {
		return
	}
	encoded, _ := json.Marshal(result)
	if err := json.Unmarshal(encoded, out); err != nil {
		r.t.Fatalf("%s result %s: %v", name, encoded, err)
	}
}

func (r fixtureRunner) request(id int64) models.HttpRequest {
	r.t.Helper()
	var req models.HttpRequest
	r.run("request.get", map[string]any{"id": id}, &req)
	return req
}

func (r fixtureRunner) requestID(name string) int64 {
	r.t.Helper()
	var id int64
	if err := r.db.QueryRow(`SELECT id FROM http_requests WHERE name = ?`, name).Scan(&id); err != nil {
		r.t.Fatalf("request %q: %v", name, err)
	}
	return id
}

func (r fixtureRunner) favouriteID() int64 {
	r.t.Helper()
	var id int64
	if err := r.db.QueryRow(`SELECT id FROM favourite_collections WHERE name = 'Daily'`).Scan(&id); err != nil {
		r.t.Fatalf("favourite: %v", err)
	}
	return id
}

func (r fixtureRunner) send(in any) (string, error) {
	raw, _ := json.Marshal(in)
	var result any
	var sendErr error
	requests.NewSend(r.db).Run(raw, commands.Observer{
		Update: func(any) {},
		Finish: func(res any, err error) { result, sendErr = res, err },
	})
	encoded, _ := json.Marshal(result)
	return string(encoded), sendErr
}

func scriptLogText(res models.HttpResponse) string {
	encoded, _ := json.Marshal(res.ScriptLog)
	return string(encoded)
}

// useFixtureKey unlocks the fixtures' secrets with the committed test key
// (a copy, which the vault may rewrite). The first fixture creates the key.
func useFixtureKey(t *testing.T, mayCreate bool) string {
	t.Helper()
	key := filepath.Join(t.TempDir(), "secret.key")
	committed := filepath.Join(fixtureDir, "secret.key")
	switch {
	case exists(committed):
		copyFile(t, committed, key)
	case !mayCreate:
		t.Fatal("missing " + committed)
	}
	secrets.SetDefault(secrets.NewVault(secrets.FileKeyStore(key)))
	return key
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func pointBackupsAt(t *testing.T, path, folder string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	settings, err := backup.LoadSettings(db)
	if err != nil {
		t.Fatal(err)
	}
	settings.Folder = folder
	if err := backup.SaveSettings(db, settings); err != nil {
		t.Fatal(err)
	}
}

func copyFixture(t *testing.T, fixture string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.db")
	copyFile(t, fixture, path)
	return path
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	data, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
