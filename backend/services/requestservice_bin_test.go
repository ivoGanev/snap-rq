package services

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening in-memory db: %v", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("enabling foreign keys: %v", err)
	}
	return db
}

func runMigrations(t *testing.T, db *sql.DB) {
	t.Helper()
	// Minimal schema for testing bin/restore.
	if _, err := db.Exec(`
		CREATE TABLE profiles (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL);
		CREATE TABLE projects (id INTEGER PRIMARY KEY AUTOINCREMENT, profile_id INTEGER NOT NULL);
		CREATE TABLE collections (id INTEGER PRIMARY KEY AUTOINCREMENT, project_id INTEGER NOT NULL, name TEXT NOT NULL);
		CREATE TABLE http_requests (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			collection_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			url TEXT NOT NULL,
			method TEXT NOT NULL,
			body TEXT,
			request_headers TEXT,
			status_code INTEGER NOT NULL DEFAULT 0,
			response_id INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY (collection_id) REFERENCES collections(id) ON DELETE CASCADE
		);
		CREATE TABLE responses (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			request_id INTEGER NOT NULL,
			headers TEXT,
			status_code INTEGER NOT NULL DEFAULT 0,
			body TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			duration_ms INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY (request_id) REFERENCES http_requests(id) ON DELETE CASCADE
		);
		CREATE TABLE favourite_collections (id INTEGER PRIMARY KEY AUTOINCREMENT, project_id INTEGER NOT NULL, name TEXT NOT NULL);
		CREATE TABLE favourite_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			favourite_collection_id INTEGER NOT NULL,
			http_request_id INTEGER NOT NULL,
			FOREIGN KEY (favourite_collection_id) REFERENCES favourite_collections(id) ON DELETE CASCADE,
			FOREIGN KEY (http_request_id) REFERENCES http_requests(id) ON DELETE CASCADE
		);
		CREATE TABLE tags (id INTEGER PRIMARY KEY AUTOINCREMENT, project_id INTEGER NOT NULL, name TEXT NOT NULL);
		CREATE TABLE request_tags (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			request_id INTEGER NOT NULL,
			tag_id INTEGER NOT NULL,
			FOREIGN KEY (request_id) REFERENCES http_requests(id) ON DELETE CASCADE,
			FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE
		);
		CREATE TABLE binned_requests (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			original_request_id INTEGER NOT NULL,
			project_id INTEGER NOT NULL,
			collection_id INTEGER NOT NULL,
			original_collection_id INTEGER NOT NULL,
			original_collection_name TEXT NOT NULL,
			name TEXT NOT NULL,
			url TEXT NOT NULL,
			method TEXT NOT NULL,
			body TEXT,
			request_headers TEXT,
			status_code INTEGER NOT NULL DEFAULT 0,
			response_id INTEGER NOT NULL DEFAULT 0,
			deleted_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			snapshot_json TEXT NOT NULL
		);
	`); err != nil {
		t.Fatalf("creating schema: %v", err)
	}
}

func TestBinRequestMovesRequestToBin(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	runMigrations(t, db)

	svc := NewRequestService(db)

	// Create profile, project, collection.
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

	// Create request and response.
	res, err = db.Exec(`
		INSERT INTO http_requests (collection_id, name, url, method, body, request_headers, status_code, response_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		collectionID, "req1", "https://example.com", "GET", "body", "headers", 200, 0)
	if err != nil {
		t.Fatalf("insert request: %v", err)
	}
	requestID, _ := res.LastInsertId()

	if _, err := db.Exec("INSERT INTO responses (request_id, headers, status_code, body, duration_ms) VALUES (?, ?, ?, ?, ?)", requestID, "h", 200, "ok", 10); err != nil {
		t.Fatalf("insert response: %v", err)
	}

	// Bin the request.
	binID, err := svc.BinRequest(requestID)
	if err != nil {
		t.Fatalf("BinRequest failed: %v", err)
	}
	if binID == 0 {
		t.Fatalf("expected non-zero bin id")
	}

	// Request should be gone.
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM http_requests WHERE id = ?", requestID).Scan(&count); err != nil {
		t.Fatalf("counting requests: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected request to be deleted, got count %d", count)
	}

	// Responses should be gone.
	if err := db.QueryRow("SELECT COUNT(*) FROM responses WHERE request_id = ?", requestID).Scan(&count); err != nil {
		t.Fatalf("counting responses: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected responses to be deleted, got count %d", count)
	}

	// Binned row should exist.
	if err := db.QueryRow("SELECT COUNT(*) FROM binned_requests WHERE id = ?", binID).Scan(&count); err != nil {
		t.Fatalf("counting binned requests: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one binned request, got count %d", count)
	}

	// Restore should recreate the request.
	restored, err := svc.RestoreBinnedRequest(binID, collectionID)
	if err != nil {
		t.Fatalf("RestoreBinnedRequest failed: %v", err)
	}
	if restored.Name != "req1" {
		t.Fatalf("unexpected restored name: %s", restored.Name)
	}

	// Binned row should be gone after restore.
	if err := db.QueryRow("SELECT COUNT(*) FROM binned_requests WHERE id = ?", binID).Scan(&count); err != nil {
		t.Fatalf("counting binned requests after restore: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected binned request to be deleted after restore, got count %d", count)
	}
}

func TestDeleteRequestBinsRequest(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	runMigrations(t, db)

	svc := NewRequestService(db)

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

	res, err = db.Exec(`
		INSERT INTO http_requests (collection_id, name, url, method, body, request_headers, status_code, response_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		collectionID, "req1", "https://example.com", "GET", "body", "headers", 200, 0)
	if err != nil {
		t.Fatalf("insert request: %v", err)
	}
	requestID, _ := res.LastInsertId()

	if err := svc.DeleteRequest(requestID); err != nil {
		t.Fatalf("DeleteRequest failed: %v", err)
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM http_requests WHERE id = ?", requestID).Scan(&count); err != nil {
		t.Fatalf("counting requests: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected request to be binned (removed from http_requests), got count %d", count)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM binned_requests").Scan(&count); err != nil {
		t.Fatalf("counting binned requests: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one binned request, got count %d", count)
	}
}

func TestBulkDeleteRequestsBinsRequests(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	runMigrations(t, db)

	svc := NewRequestService(db)

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

	var ids []int64
	for i := 0; i < 3; i++ {
		res, err := db.Exec(`
			INSERT INTO http_requests (collection_id, name, url, method, body, request_headers, status_code, response_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			collectionID, "req", "https://example.com", "GET", "body", "headers", 200, 0)
		if err != nil {
			t.Fatalf("insert request: %v", err)
		}
		id, _ := res.LastInsertId()
		ids = append(ids, id)
	}

	if err := svc.BulkDeleteRequests(ids); err != nil {
		t.Fatalf("BulkDeleteRequests failed: %v", err)
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM http_requests").Scan(&count); err != nil {
		t.Fatalf("counting requests: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected all requests to be removed, got count %d", count)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM binned_requests").Scan(&count); err != nil {
		t.Fatalf("counting binned requests: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected 3 binned requests, got count %d", count)
	}
}
