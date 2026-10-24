package database

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigrateAddsResponseCreatedAtColumn(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening in-memory db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`
		CREATE TABLE responses (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			request_id INTEGER NOT NULL,
			headers TEXT,
			status_code INTEGER NOT NULL DEFAULT 0,
			body TEXT
		);
		INSERT INTO responses (request_id, headers, status_code, body) VALUES (1, '', 200, 'ok');
	`); err != nil {
		t.Fatalf("setting up legacy responses schema: %v", err)
	}

	if err := migrate(db); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	if err := addResponseCreatedAtColumn(db); err != nil {
		t.Fatalf("addResponseCreatedAtColumn failed: %v", err)
	}

	var createdAt string
	if err := db.QueryRow("SELECT created_at FROM responses WHERE id = 1").Scan(&createdAt); err != nil {
		t.Fatalf("reading created_at: %v", err)
	}
	if createdAt == "" {
		t.Errorf("expected created_at to be populated, got empty")
	}
}

func TestMigrateCreatesSecureDefaultAppSettings(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening in-memory db: %v", err)
	}
	defer db.Close()

	if err := migrate(db); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}

	var value string
	if err := db.QueryRow(
		"SELECT value FROM app_settings WHERE key = 'validate_ssl_certificates'",
	).Scan(&value); err != nil {
		t.Fatalf("reading default app setting: %v", err)
	}
	if value != "true" {
		t.Fatalf("expected secure default app setting, got %q", value)
	}
}

func TestMigrateCollectionAppearancesFromLegacyIconIDColumnOnly(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening in-memory db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`
		CREATE TABLE collections (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			icon_id TEXT NOT NULL DEFAULT ''
		);
		INSERT INTO collections (project_id, name, icon_id) VALUES (1, 'with-icon', 'heart');
		INSERT INTO collections (project_id, name, icon_id) VALUES (2, 'no-icon', '');
	`); err != nil {
		t.Fatalf("setting up legacy schema: %v", err)
	}

	if err := migrate(db); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}

	rows, err := db.Query("SELECT collection_id, appearance_type, appearance_value FROM collection_appearances ORDER BY collection_id")
	if err != nil {
		t.Fatalf("reading appearances: %v", err)
	}
	defer rows.Close()

	var appearances []struct {
		id    int64
		typ   string
		value string
	}
	for rows.Next() {
		var a struct {
			id    int64
			typ   string
			value string
		}
		if err := rows.Scan(&a.id, &a.typ, &a.value); err != nil {
			t.Fatalf("scanning appearance: %v", err)
		}
		appearances = append(appearances, a)
	}

	if len(appearances) != 2 {
		t.Fatalf("expected 2 appearances, got %d", len(appearances))
	}

	if appearances[0].typ != "icon" || appearances[0].value != "heart" {
		t.Errorf("expected icon/heart for first collection, got %s/%s", appearances[0].typ, appearances[0].value)
	}
	if appearances[1].typ != "icon" || appearances[1].value != "default" {
		t.Errorf("expected icon/default for second collection, got %s/%s", appearances[1].typ, appearances[1].value)
	}
}

// Regression test: on a database created before the user_sort_order columns
// existed, the schema migration must not reference those columns (e.g. via
// CREATE INDEX) before addUserSortOrderColumns has added them. This exact
// sequence broke app startup with "no such column: user_sort_order".
func TestMigrateLegacySchemaWithoutUserSortOrder(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening in-memory db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`
		CREATE TABLE profiles (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL);
		CREATE TABLE projects (id INTEGER PRIMARY KEY AUTOINCREMENT, profile_id INTEGER NOT NULL, name TEXT NOT NULL);
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
			FOREIGN KEY (collection_id) REFERENCES collections(id) ON DELETE CASCADE
		);
		CREATE TABLE responses (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			request_id INTEGER NOT NULL,
			headers TEXT,
			status_code INTEGER NOT NULL DEFAULT 0,
			body TEXT
		);
		CREATE TABLE favourite_collections (id INTEGER PRIMARY KEY AUTOINCREMENT, project_id INTEGER NOT NULL, name TEXT NOT NULL);
		CREATE TABLE favourite_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			favourite_collection_id INTEGER NOT NULL,
			http_request_id INTEGER NOT NULL,
			added_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			sort_order INTEGER NOT NULL DEFAULT 0,
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
		INSERT INTO collections (project_id, name) VALUES (1, 'legacy');
		INSERT INTO http_requests (collection_id, name, url, method) VALUES (1, 'legacy request', 'http://x', 'GET');
		INSERT INTO favourite_items (favourite_collection_id, http_request_id, sort_order) VALUES (1, 1, 7);
	`); err != nil {
		t.Fatalf("setting up legacy schema: %v", err)
	}

	// Same sequence Open() runs: schema pass first, column migration after.
	if err := migrate(db); err != nil {
		t.Fatalf("migrate failed on legacy schema: %v", err)
	}
	if err := addUserSortOrderColumns(db); err != nil {
		t.Fatalf("addUserSortOrderColumns failed: %v", err)
	}

	for _, table := range []string{"http_requests", "request_tags"} {
		if !columnExists(db, table, "user_sort_order") {
			t.Errorf("expected %s.user_sort_order to exist after migration", table)
		}
	}

	// favourite_items.sort_order is renamed (not duplicated), values preserved.
	if columnExists(db, "favourite_items", "sort_order") {
		t.Errorf("expected favourite_items.sort_order to be renamed")
	}
	var order int
	if err := db.QueryRow("SELECT user_sort_order FROM favourite_items WHERE id = 1").Scan(&order); err != nil {
		t.Fatalf("reading renamed favourite order: %v", err)
	}
	if order != 7 {
		t.Errorf("expected favourite order value 7 to be preserved, got %d", order)
	}

	for _, index := range []string{"idx_http_requests_collection_sort", "idx_request_tags_tag_sort"} {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?", index).Scan(&count); err != nil {
			t.Fatalf("checking index %s: %v", index, err)
		}
		if count != 1 {
			t.Errorf("expected index %s to exist after migration", index)
		}
	}
}
