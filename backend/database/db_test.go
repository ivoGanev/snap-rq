package database

import (
	"database/sql"
	"path/filepath"
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
	if appearances[1].typ != "icon" || appearances[1].value != "ico_1a9d4" {
		t.Errorf("expected icon/ico_1a9d4 for second collection, got %s/%s", appearances[1].typ, appearances[1].value)
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

func TestWidenBodyTypeCheckRebuildsTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	defer db.Close()

	// Legacy schema: body_type restricted to json/text, with body_form and a
	// body_mode column carrying the previously-stored form mode.
	if _, err := db.Exec(`
		CREATE TABLE collections (id INTEGER PRIMARY KEY AUTOINCREMENT, project_id INTEGER NOT NULL, name TEXT NOT NULL);
		CREATE TABLE http_requests (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			collection_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			url TEXT NOT NULL,
			method TEXT NOT NULL,
			body TEXT,
			body_type TEXT NOT NULL DEFAULT 'json' CHECK(body_type IN ('json', 'text')),
			request_headers TEXT,
			status_code INTEGER NOT NULL DEFAULT 0,
			response_id INTEGER NOT NULL DEFAULT 0,
			user_sort_order INTEGER NOT NULL DEFAULT 0,
			user_color TEXT,
			last_used_at INTEGER NOT NULL DEFAULT 0,
			body_form TEXT,
			body_mode TEXT,
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
		CREATE TABLE binned_requests (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			original_request_id INTEGER NOT NULL,
			project_id INTEGER NOT NULL,
			collection_id INTEGER NOT NULL DEFAULT 0,
			original_collection_id INTEGER NOT NULL,
			original_collection_name TEXT NOT NULL,
			name TEXT NOT NULL,
			url TEXT NOT NULL,
			method TEXT NOT NULL,
			body TEXT,
			body_type TEXT NOT NULL DEFAULT 'json' CHECK(body_type IN ('json', 'text')),
			request_headers TEXT,
			status_code INTEGER NOT NULL DEFAULT 0,
			response_id INTEGER NOT NULL DEFAULT 0,
			deleted_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			snapshot_json TEXT NOT NULL,
			body_form TEXT,
			body_mode TEXT
		);
		INSERT INTO collections (project_id, name) VALUES (1, 'c');
		INSERT INTO http_requests (collection_id, name, url, method, body, body_type) VALUES (1, 'json req', 'http://x', 'POST', '{"a":1}', 'json');
		INSERT INTO http_requests (collection_id, name, url, method, body_form, body_mode) VALUES (1, 'form req', 'http://x', 'POST', '[{"key":"a","type":"text","value":"1","enabled":true}]', 'form-data');
	`); err != nil {
		t.Fatalf("setting up legacy schema: %v", err)
	}

	if err := widenBodyTypeCheck(db); err != nil {
		t.Fatalf("widenBodyTypeCheck failed: %v", err)
	}

	// The legacy form row is reclassified as form-data.
	var bodyType string
	if err := db.QueryRow("SELECT body_type FROM http_requests WHERE name = 'form req'").Scan(&bodyType); err != nil {
		t.Fatalf("reading form req: %v", err)
	}
	if bodyType != "form-data" {
		t.Errorf("expected form req body_type 'form-data', got %q", bodyType)
	}

	// The json row and its body survive the rebuild.
	var jsonBody string
	if err := db.QueryRow("SELECT body FROM http_requests WHERE name = 'json req'").Scan(&jsonBody); err != nil {
		t.Fatalf("reading json req: %v", err)
	}
	if jsonBody != `{"a":1}` {
		t.Errorf("expected json body preserved, got %q", jsonBody)
	}

	// body_mode is gone; form-data is now a valid body_type value.
	if columnExists(db, "http_requests", "body_mode") {
		t.Error("expected body_mode column to be dropped")
	}
	if _, err := db.Exec("UPDATE http_requests SET body_type = 'form-data' WHERE name = 'json req'"); err != nil {
		t.Errorf("expected widened CHECK to accept form-data, got: %v", err)
	}

	// Foreign keys still cascade after the rebuild.
	if _, err := db.Exec("INSERT INTO responses (request_id, body) VALUES (1, 'ok')"); err != nil {
		t.Fatalf("inserting response: %v", err)
	}
	if _, err := db.Exec("DELETE FROM collections WHERE id = 1"); err != nil {
		t.Fatalf("deleting collection: %v", err)
	}
	var respCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM responses").Scan(&respCount); err != nil {
		t.Fatalf("counting responses: %v", err)
	}
	if respCount != 0 {
		t.Errorf("expected cascade delete of responses, got %d remaining", respCount)
	}
}

func TestMigrateRequestUsageIntoHttpRequests(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening in-memory db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`
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
			response_id INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE request_usage (
			request_id INTEGER PRIMARY KEY,
			last_used_at INTEGER NOT NULL
		);
		INSERT INTO http_requests (collection_id, name, url, method) VALUES (1, 'first', 'http://a', 'GET');
		INSERT INTO http_requests (collection_id, name, url, method) VALUES (1, 'second', 'http://b', 'GET');
		INSERT INTO request_usage (request_id, last_used_at) VALUES (1, 1700000000000000000);
	`); err != nil {
		t.Fatalf("setting up legacy schema: %v", err)
	}

	if err := migrate(db); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	if err := addRequestLastUsedAt(db); err != nil {
		t.Fatalf("addRequestLastUsedAt failed: %v", err)
	}

	if !columnExists(db, "http_requests", "last_used_at") {
		t.Fatal("expected http_requests.last_used_at after migration")
	}
	if tableExists(db, "request_usage") {
		t.Error("expected request_usage to be dropped")
	}

	var used int64
	if err := db.QueryRow(`SELECT last_used_at FROM http_requests WHERE id = 1`).Scan(&used); err != nil {
		t.Fatalf("reading copied last_used_at: %v", err)
	}
	if used != 1700000000000000000 {
		t.Errorf("expected copied usage, got %d", used)
	}
	var unused int64
	if err := db.QueryRow(`SELECT last_used_at FROM http_requests WHERE id = 2`).Scan(&unused); err != nil {
		t.Fatalf("reading untouched last_used_at: %v", err)
	}
	if unused != 0 {
		t.Errorf("expected 0 for a request with no usage, got %d", unused)
	}

	var count int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_http_requests_last_used_at'",
	).Scan(&count); err != nil {
		t.Fatalf("checking index: %v", err)
	}
	if count != 1 {
		t.Error("expected the last_used_at index to exist")
	}
}

func TestMigrateUserColorsToSlots(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening in-memory db: %v", err)
	}
	defer db.Close()
	// One connection, so every statement sees the same in-memory database.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`
		CREATE TABLE http_requests (id INTEGER PRIMARY KEY, user_color TEXT);
		CREATE TABLE favourite_items (id INTEGER PRIMARY KEY, user_color TEXT);
		CREATE TABLE request_tags (id INTEGER PRIMARY KEY, user_color TEXT);
		CREATE TABLE undo_log (id INTEGER PRIMARY KEY, input TEXT, snapshot TEXT, result TEXT);

		INSERT INTO http_requests (user_color) VALUES ('#AECBFA'), ('#b6e2c6'), ('#F7E6A3'), ('primary'), ('#123456'), (NULL);
		INSERT INTO favourite_items (user_color) VALUES ('#AECBFA');
		INSERT INTO request_tags (user_color) VALUES ('#B6E2C6');
		INSERT INTO undo_log (input, snapshot, result) VALUES
			('{"color":"#AECBFA"}', '[{"id":1,"user_color":"#F7E6A3"},{"id":2,"user_color":null}]', 'null');
	`); err != nil {
		t.Fatalf("seeding legacy colours: %v", err)
	}

	// Running twice proves the migration is idempotent.
	for i := 0; i < 2; i++ {
		if err := migrateUserColorsToSlots(db); err != nil {
			t.Fatalf("migrateUserColorsToSlots run %d: %v", i+1, err)
		}
	}

	rows, err := db.Query(`SELECT user_color FROM http_requests ORDER BY id`)
	if err != nil {
		t.Fatalf("reading colours: %v", err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var c sql.NullString
		if err := rows.Scan(&c); err != nil {
			t.Fatalf("scanning colour: %v", err)
		}
		got = append(got, c.String)
	}
	want := []string{"primary", "secondary", "tertiary", "primary", "#123456", ""}
	if len(got) != len(want) {
		t.Fatalf("got %d colours, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("http_requests colour %d = %q, want %q", i, got[i], want[i])
		}
	}

	var fav, tag, input, snapshot string
	if err := db.QueryRow(`SELECT user_color FROM favourite_items`).Scan(&fav); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT user_color FROM request_tags`).Scan(&tag); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT input, snapshot FROM undo_log`).Scan(&input, &snapshot); err != nil {
		t.Fatal(err)
	}
	if fav != "primary" || tag != "secondary" {
		t.Errorf("favourite/tag colours = %q/%q, want primary/secondary", fav, tag)
	}
	if input != `{"color":"primary"}` {
		t.Errorf("undo_log input = %s", input)
	}
	if snapshot != `[{"id":1,"user_color":"tertiary"},{"id":2,"user_color":null}]` {
		t.Errorf("undo_log snapshot = %s", snapshot)
	}
}
