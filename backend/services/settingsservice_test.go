package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"snap-rq/backend/commands"
	"snap-rq/backend/commands/runnables"
	"snap-rq/backend/models"
)

func TestSettingsServiceDefaultsAndUpdate(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE app_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatalf("creating app_settings table: %v", err)
	}

	svc := NewSettingsService(db)

	settings, err := svc.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings failed: %v", err)
	}
	if !settings.ValidateSSLCertificates {
		t.Fatal("expected SSL validation to default to true")
	}

	updated, err := svc.UpdateSettings(models.AppSettings{ValidateSSLCertificates: false})
	if err != nil {
		t.Fatalf("UpdateSettings failed: %v", err)
	}
	if updated.ValidateSSLCertificates {
		t.Fatal("expected updated SSL validation to be false")
	}

	settings, err = svc.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings after update failed: %v", err)
	}
	if settings.ValidateSSLCertificates {
		t.Fatal("expected persisted SSL validation to be false")
	}
}

// The send flow lives in the command framework now; the SSL setting it
// honors is exercised through the request.send command.
func TestExecuteRequestHonorsSSLValidationSetting(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	db := newTestDB(t)
	defer db.Close()
	runMigrations(t, db)
	if _, err := db.Exec(`CREATE TABLE app_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatalf("creating app_settings table: %v", err)
	}

	res, err := db.Exec("INSERT INTO profiles (name) VALUES (?)", "p")
	if err != nil {
		t.Fatalf("insert profile: %v", err)
	}
	profileID, _ := res.LastInsertId()
	res, err = db.Exec("INSERT INTO projects (profile_id) VALUES (?)", profileID)
	if err != nil {
		t.Fatalf("insert project: %v", err)
	}
	projectID, _ := res.LastInsertId()
	res, err = db.Exec("INSERT INTO collections (project_id, name) VALUES (?, ?)", projectID, "c1")
	if err != nil {
		t.Fatalf("insert collection: %v", err)
	}
	collectionID, _ := res.LastInsertId()
	res, err = db.Exec(
		`INSERT INTO http_requests (collection_id, name, url, method, body, request_headers) VALUES (?, ?, ?, ?, ?, ?)`,
		collectionID, "tls", server.URL, "GET", "", "",
	)
	if err != nil {
		t.Fatalf("insert request: %v", err)
	}
	requestID, _ := res.LastInsertId()

	engine := commands.NewEngineWithManifest(db, testManifest())
	if err := engine.Register(&runnables.SendRequest{}); err != nil {
		t.Fatalf("registering send command: %v", err)
	}

	send := func() models.HttpResponse {
		t.Helper()
		// The caller supplies the full request content (the frontend
		// interpolates environment variables before sending).
		out, err := engine.Run("request.send", mustJSON(t, map[string]any{
			"id": requestID, "url": server.URL, "method": "GET",
		}))
		if err != nil {
			t.Fatalf("send failed: %v", err)
		}
		resp, ok := out.(models.HttpResponse)
		if !ok {
			t.Fatalf("expected an HttpResponse back, got %T", out)
		}
		return resp
	}

	// With validation on, the self-signed certificate fails.
	if response := send(); response.StatusCode != 0 {
		t.Fatalf("expected self-signed certificate to fail, got status %d", response.StatusCode)
	}

	settingsSvc := NewSettingsService(db)
	if _, err := settingsSvc.UpdateSettings(models.AppSettings{ValidateSSLCertificates: false}); err != nil {
		t.Fatalf("disabling SSL validation failed: %v", err)
	}

	// With validation off, the request succeeds.
	if response := send(); response.StatusCode != http.StatusOK {
		t.Fatalf("expected request to succeed with validation disabled, got status %d: %s",
			response.StatusCode, response.Body)
	}
}

// testManifest declares the edges this test schema really has.
func testManifest() *commands.Manifest {
	return &commands.Manifest{
		Tables: map[string]commands.TableRule{
			"profiles": {Entity: "profile", Children: map[string]commands.ChildRule{
				"projects": {},
			}},
			"projects": {Entity: "project", Children: map[string]commands.ChildRule{
				"collections": {},
			}},
			"collections": {Entity: "collection", Children: map[string]commands.ChildRule{
				"http_requests": {},
			}},
			"http_requests": {Entity: "request", Children: map[string]commands.ChildRule{
				"responses":       {},
				"request_tags":    {},
				"favourite_items": {},
				"request_usage":   {Restore: commands.NoRestore},
			}},
			"responses":    {Entity: "response"},
			"request_tags": {Entity: "request_tag", Children: nil},
			"tags": {Entity: "tag", Children: map[string]commands.ChildRule{
				"request_tags": {},
			}},
			"favourite_collections": {Entity: "favourite_collection", Children: map[string]commands.ChildRule{
				"favourite_items": {},
			}},
			"favourite_items": {Entity: "favourite_item"},
			"request_usage":   {Entity: "request_usage"},
			"binned_requests": {Entity: "bin"},
			"app_settings":    {Entity: "settings"},
		},
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encoding payload: %v", err)
	}
	return raw
}
