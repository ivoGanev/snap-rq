package services

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// newTestDB opens an in-memory database with foreign keys on, for the
// service tests that remain.
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

// runMigrations creates the minimal schema the remaining service tests
// need.
func runMigrations(t *testing.T, db *sql.DB) {
	t.Helper()
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
			body_type TEXT NOT NULL DEFAULT 'json',
			request_headers TEXT,
			status_code INTEGER NOT NULL DEFAULT 0,
			response_id INTEGER NOT NULL DEFAULT 0,
			user_sort_order INTEGER NOT NULL DEFAULT 0,
			user_color TEXT,
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
		CREATE TABLE request_usage (
			request_id INTEGER PRIMARY KEY,
			last_used_at INTEGER NOT NULL,
			FOREIGN KEY (request_id) REFERENCES http_requests(id) ON DELETE CASCADE
		);
		CREATE TABLE favourite_collections (id INTEGER PRIMARY KEY AUTOINCREMENT, project_id INTEGER NOT NULL, name TEXT NOT NULL);
		CREATE TABLE favourite_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			favourite_collection_id INTEGER NOT NULL,
			http_request_id INTEGER NOT NULL,
			user_sort_order INTEGER NOT NULL DEFAULT 0,
			user_color TEXT,
			FOREIGN KEY (favourite_collection_id) REFERENCES favourite_collections(id) ON DELETE CASCADE,
			FOREIGN KEY (http_request_id) REFERENCES http_requests(id) ON DELETE CASCADE
		);
		CREATE TABLE tags (id INTEGER PRIMARY KEY AUTOINCREMENT, project_id INTEGER NOT NULL, name TEXT NOT NULL);
		CREATE TABLE tag_appearances (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tag_id INTEGER NOT NULL,
			appearance_type TEXT NOT NULL,
			appearance_value TEXT NOT NULL
		);
		CREATE TABLE request_tags (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			request_id INTEGER NOT NULL,
			tag_id INTEGER NOT NULL,
			user_sort_order INTEGER NOT NULL DEFAULT 0,
			user_color TEXT,
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
			body_type TEXT NOT NULL DEFAULT 'json',
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
