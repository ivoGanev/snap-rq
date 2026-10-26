package services

import (
	"net/http"
	"net/http/httptest"
	"testing"

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

	requestSvc := NewRequestService(db)
	executeReq, err := requestSvc.GetRequest(requestID)
	if err != nil {
		t.Fatalf("loading request: %v", err)
	}

	response, err := requestSvc.ExecuteRequest(executeReq)
	if err != nil {
		t.Fatalf("ExecuteRequest with validation enabled failed: %v", err)
	}
	if response.StatusCode != 0 {
		t.Fatalf("expected self-signed certificate to fail, got status %d", response.StatusCode)
	}

	settingsSvc := NewSettingsService(db)
	if _, err := settingsSvc.UpdateSettings(models.AppSettings{ValidateSSLCertificates: false}); err != nil {
		t.Fatalf("disabling SSL validation failed: %v", err)
	}

	response, err = requestSvc.ExecuteRequest(executeReq)
	if err != nil {
		t.Fatalf("ExecuteRequest with validation disabled failed: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected request to succeed with validation disabled, got status %d: %s", response.StatusCode, response.Body)
	}
}
