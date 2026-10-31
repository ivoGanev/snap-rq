package runnables

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"

	_ "modernc.org/sqlite"
)

// newTestEngine builds the real engine (embedded manifest, full catalog)
// against an in-memory database with the real schema, exactly the way the
// app boots.
func newTestEngine(t *testing.T) (*commands.Engine, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening in-memory db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("enabling foreign keys: %v", err)
	}
	createSchema(t, db)

	engine, err := commands.NewEngine(db)
	if err != nil {
		t.Fatalf("building the engine: %v", err)
	}
	if err := engine.Register(Catalog...); err != nil {
		t.Fatalf("registering the catalog: %v", err)
	}
	if err := engine.Start(); err != nil {
		t.Fatalf("starting (manifest vs schema validation): %v", err)
	}
	return engine, db
}

// createSchema mirrors the production schema, including every cascade the
// manifest declares — Start() refuses to run if they disagree.
func createSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`
		CREATE TABLE profiles (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL);
		CREATE TABLE projects (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			profile_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			FOREIGN KEY (profile_id) REFERENCES profiles(id) ON DELETE CASCADE
		);
		CREATE TABLE collections (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
		);
		CREATE TABLE collection_appearances (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			collection_id INTEGER NOT NULL,
			appearance_type TEXT NOT NULL,
			appearance_value TEXT NOT NULL,
			FOREIGN KEY (collection_id) REFERENCES collections(id) ON DELETE CASCADE
		);
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
		CREATE TABLE environments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
		);
		CREATE TABLE environment_variables (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			environment_id INTEGER NOT NULL,
			key TEXT NOT NULL,
			value TEXT NOT NULL,
			FOREIGN KEY (environment_id) REFERENCES environments(id) ON DELETE CASCADE
		);
		CREATE TABLE favourite_collections (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
		);
		CREATE TABLE favourite_appearances (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			favourite_collection_id INTEGER NOT NULL,
			appearance_type TEXT NOT NULL,
			appearance_value TEXT NOT NULL,
			FOREIGN KEY (favourite_collection_id) REFERENCES favourite_collections(id) ON DELETE CASCADE
		);
		CREATE TABLE favourite_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			favourite_collection_id INTEGER NOT NULL,
			http_request_id INTEGER NOT NULL,
			added_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			user_sort_order INTEGER NOT NULL DEFAULT 0,
			user_color TEXT,
			FOREIGN KEY (favourite_collection_id) REFERENCES favourite_collections(id) ON DELETE CASCADE,
			FOREIGN KEY (http_request_id) REFERENCES http_requests(id) ON DELETE CASCADE
		);
		CREATE TABLE tags (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
		);
		CREATE TABLE tag_appearances (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tag_id INTEGER NOT NULL,
			appearance_type TEXT NOT NULL,
			appearance_value TEXT NOT NULL,
			FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE
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
			collection_id INTEGER NOT NULL DEFAULT 0,
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
		CREATE TABLE app_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
	`); err != nil {
		t.Fatalf("creating schema: %v", err)
	}
}

func run(t *testing.T, engine *commands.Engine, name string, input any) any {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("encoding input: %v", err)
	}
	out, err := engine.Run(name, raw)
	if err != nil {
		t.Fatalf("running %s: %v", name, err)
	}
	return out
}

func count(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("counting: %v", err)
	}
	return n
}

// seedWorkspace creates profile/project/collection and returns their ids.
func seedWorkspace(t *testing.T, db *sql.DB) (projectID, collectionID int64) {
	t.Helper()
	result, err := db.Exec(`
		INSERT INTO profiles (name) VALUES ('p');
		INSERT INTO projects (profile_id, name) VALUES (1, 'proj');
		INSERT INTO collections (project_id, name) VALUES (1, 'col');
	`)
	if err != nil {
		t.Fatalf("seeding workspace: %v", err)
	}
	_ = result
	return 1, 1
}

func TestDeleteRequestsUndoRestoresTheWholeTree(t *testing.T) {
	engine, db := newTestEngine(t)
	_, collectionID := seedWorkspace(t, db)

	if _, err := db.Exec(`
		INSERT INTO http_requests (collection_id, name, url, method, response_id) VALUES (1, 'victim', 'http://x', 'GET', 1);
		INSERT INTO responses (request_id, status_code, body) VALUES (1, 200, 'payload');
		INSERT INTO tags (project_id, name) VALUES (1, 'red');
		INSERT INTO request_tags (request_id, tag_id) VALUES (1, 1);
		INSERT INTO favourite_collections (project_id, name) VALUES (1, 'fav');
		INSERT INTO favourite_appearances (favourite_collection_id, appearance_type, appearance_value) VALUES (1, 'icon', 'ico_1a9d4');
		INSERT INTO favourite_items (favourite_collection_id, http_request_id) VALUES (1, 1);
	`); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	_ = collectionID

	run(t, engine, "request.delete", map[string]any{"ids": []int64{1}})

	if count(t, db, "SELECT COUNT(*) FROM http_requests") != 0 {
		t.Fatalf("expected the request binned")
	}
	if count(t, db, "SELECT COUNT(*) FROM binned_requests") != 1 {
		t.Fatalf("expected one trash row")
	}

	if _, err := engine.Undo(); err != nil {
		t.Fatalf("undoing delete: %v", err)
	}

	// The request is back under its original id, with its response, tag
	// and favourite memberships; the trash row is gone.
	if count(t, db, "SELECT COUNT(*) FROM http_requests WHERE id = 1 AND response_id = 1") != 1 {
		t.Fatalf("expected the request restored with its response link")
	}
	if count(t, db, "SELECT COUNT(*) FROM responses WHERE id = 1") != 1 {
		t.Fatalf("expected the response restored")
	}
	if count(t, db, "SELECT COUNT(*) FROM request_tags WHERE request_id = 1") != 1 {
		t.Fatalf("expected the tag membership restored")
	}
	if count(t, db, "SELECT COUNT(*) FROM favourite_items WHERE http_request_id = 1") != 1 {
		t.Fatalf("expected the favourite membership restored")
	}
	if count(t, db, "SELECT COUNT(*) FROM binned_requests") != 0 {
		t.Fatalf("expected the trash row removed by the undo")
	}
}

func TestCreateRequestUndoRemovesIt(t *testing.T) {
	engine, db := newTestEngine(t)
	_, _ = seedWorkspace(t, db)

	created := run(t, engine, "request.create", map[string]any{
		"collection_id": 1, "name": "new", "url": "http://x", "method": "GET",
	})
	req, ok := created.(models.HttpRequest)
	if !ok {
		t.Fatalf("expected an HttpRequest back, got %T", created)
	}
	if req.ID == 0 {
		t.Fatalf("expected an id, got %v", req)
	}

	if _, err := engine.Undo(); err != nil {
		t.Fatalf("undoing create: %v", err)
	}
	if count(t, db, "SELECT COUNT(*) FROM http_requests") != 0 {
		t.Fatalf("expected the created request removed by the undo")
	}

	// Redo brings it back under the same id.
	if _, err := engine.Redo(); err != nil {
		t.Fatalf("redoing create: %v", err)
	}
	if count(t, db, "SELECT COUNT(*) FROM http_requests WHERE id = ?", req.ID) != 1 {
		t.Fatalf("expected the request back under its original id")
	}
}

func TestUpdateRequestUndoKeepsSendColumns(t *testing.T) {
	engine, db := newTestEngine(t)
	_, _ = seedWorkspace(t, db)
	if _, err := db.Exec(`
		INSERT INTO http_requests (collection_id, name, url, method, status_code) VALUES (1, 'old', 'http://x', 'GET', 0)
	`); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	run(t, engine, "request.update", map[string]any{
		"id": 1, "collection_id": 1, "name": "new", "url": "http://y", "method": "POST",
	})

	// A send lands in between and writes the send flow's columns.
	run(t, engine, "request.record_response", map[string]any{
		"request_id": 1, "status_code": 500, "response_id": 9,
	})

	if _, err := engine.Undo(); err != nil {
		t.Fatalf("undoing update: %v", err)
	}

	var name string
	var statusCode int
	var responseID int64
	if err := db.QueryRow(
		`SELECT name, status_code, response_id FROM http_requests WHERE id = 1`,
	).Scan(&name, &statusCode, &responseID); err != nil {
		t.Fatalf("reading the request: %v", err)
	}
	if name != "old" {
		t.Errorf("expected the old name back, got %q", name)
	}
	if statusCode != 500 || responseID != 9 {
		t.Errorf("expected the send's status columns to survive the undo, got %d/%d",
			statusCode, responseID)
	}
}

func TestBulkSetUndoRestoresEveryValue(t *testing.T) {
	engine, db := newTestEngine(t)
	_, _ = seedWorkspace(t, db)
	if _, err := db.Exec(`
		INSERT INTO http_requests (collection_id, name, url, method) VALUES
			(1, 'a', 'http://a', 'GET'),
			(1, 'b', 'http://b', 'GET')
	`); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	run(t, engine, "request.bulk_set", map[string]any{
		"ids": []int64{1, 2}, "field": "name", "value": "renamed",
	})
	if count(t, db, "SELECT COUNT(*) FROM http_requests WHERE name = 'renamed'") != 2 {
		t.Fatalf("expected both requests renamed")
	}

	if _, err := engine.Undo(); err != nil {
		t.Fatalf("undoing bulk set: %v", err)
	}
	if count(t, db, "SELECT COUNT(*) FROM http_requests WHERE name IN ('a', 'b')") != 2 {
		t.Fatalf("expected both original names restored by one undo")
	}
}

func TestDuplicateRequestUndoRemovesTheCopyAndItsMemberships(t *testing.T) {
	engine, db := newTestEngine(t)
	_, _ = seedWorkspace(t, db)
	if _, err := db.Exec(`
		INSERT INTO http_requests (collection_id, name, url, method) VALUES (1, 'orig', 'http://x', 'GET');
		INSERT INTO tags (project_id, name) VALUES (1, 'red');
		INSERT INTO request_tags (request_id, tag_id) VALUES (1, 1);
		INSERT INTO favourite_collections (project_id, name) VALUES (1, 'fav');
		INSERT INTO favourite_items (favourite_collection_id, http_request_id) VALUES (1, 1);
	`); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	run(t, engine, "request.duplicate", map[string]any{"id": 1})
	if count(t, db, "SELECT COUNT(*) FROM http_requests") != 2 {
		t.Fatalf("expected the duplicate created")
	}
	if count(t, db, "SELECT COUNT(*) FROM request_tags") != 2 {
		t.Fatalf("expected the tag membership copied")
	}
	if count(t, db, "SELECT COUNT(*) FROM favourite_items") != 2 {
		t.Fatalf("expected the favourite membership copied")
	}

	if _, err := engine.Undo(); err != nil {
		t.Fatalf("undoing duplicate: %v", err)
	}
	if count(t, db, "SELECT COUNT(*) FROM http_requests") != 1 {
		t.Fatalf("expected the copy removed")
	}
	if count(t, db, "SELECT COUNT(*) FROM request_tags") != 1 {
		t.Fatalf("expected the copied membership removed")
	}
	if count(t, db, "SELECT COUNT(*) FROM favourite_items") != 1 {
		t.Fatalf("expected the copied favourite membership removed")
	}
}

func TestUpdateOrderRoundTripsThroughItsOwnUndo(t *testing.T) {
	engine, db := newTestEngine(t)
	_, _ = seedWorkspace(t, db)
	if _, err := db.Exec(`
		INSERT INTO http_requests (collection_id, name, url, method, user_sort_order) VALUES
			(1, 'a', 'http://a', 'GET', 10),
			(1, 'b', 'http://b', 'GET', 20),
			(1, 'c', 'http://c', 'GET', 30)
	`); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	run(t, engine, "request.update_order", map[string]any{
		"collection_id": 1, "ordered_ids": []int64{3, 1, 2},
	})

	orders := func() string {
		rows, err := db.Query(`SELECT user_sort_order FROM http_requests ORDER BY id`)
		if err != nil {
			t.Fatalf("reading orders: %v", err)
		}
		defer rows.Close()
		var orders []int
		for rows.Next() {
			var order int
			_ = rows.Scan(&order)
			orders = append(orders, order)
		}
		return fmt.Sprint(orders)
	}

	if got := orders(); got != "[1 2 0]" {
		t.Fatalf("expected the new order applied, got %s", got)
	}

	if _, err := engine.Undo(); err != nil {
		t.Fatalf("undoing the reorder: %v", err)
	}
	if got := orders(); got != "[10 20 30]" {
		t.Fatalf("expected the previous order restored by the bulk undo, got %s", got)
	}
}

func TestBinRestoreIsUndoable(t *testing.T) {
	engine, db := newTestEngine(t)
	_, _ = seedWorkspace(t, db)
	if _, err := db.Exec(`
		INSERT INTO http_requests (collection_id, name, url, method, response_id) VALUES (1, 'victim', 'http://x', 'GET', 1);
		INSERT INTO responses (request_id, status_code, body) VALUES (1, 200, 'payload')
	`); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	// Delete to the trash, then restore from it via the command.
	run(t, engine, "request.delete", map[string]any{"ids": []int64{1}})
	var binID int64
	if err := db.QueryRow("SELECT id FROM binned_requests").Scan(&binID); err != nil {
		t.Fatalf("reading the bin row: %v", err)
	}

	run(t, engine, "bin.restore", map[string]any{
		"items": []map[string]any{{"bin_id": binID, "collection_id": 1}},
	})
	if count(t, db, "SELECT COUNT(*) FROM http_requests") != 1 {
		t.Fatalf("expected the request restored from the trash")
	}
	if count(t, db, "SELECT COUNT(*) FROM responses") != 1 {
		t.Fatalf("expected the response history restored from the trash")
	}
	if count(t, db, "SELECT COUNT(*) FROM binned_requests") != 0 {
		t.Fatalf("expected the bin row consumed")
	}

	// Undoing the restore deletes the recreated rows and puts the bin row
	// back.
	if _, err := engine.Undo(); err != nil {
		t.Fatalf("undoing the restore: %v", err)
	}
	if count(t, db, "SELECT COUNT(*) FROM http_requests") != 0 {
		t.Fatalf("expected the restored request removed by the undo")
	}
	if count(t, db, "SELECT COUNT(*) FROM binned_requests") != 1 {
		t.Fatalf("expected the bin row back after the undo")
	}
}

func TestPermanentDeleteForgetsUndoSteps(t *testing.T) {
	engine, db := newTestEngine(t)
	_, _ = seedWorkspace(t, db)
	if _, err := db.Exec(`
		INSERT INTO http_requests (collection_id, name, url, method) VALUES (1, 'victim', 'http://x', 'GET')
	`); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	run(t, engine, "request.delete", map[string]any{"ids": []int64{1}})
	var binID int64
	if err := db.QueryRow("SELECT id FROM binned_requests").Scan(&binID); err != nil {
		t.Fatalf("reading the bin row: %v", err)
	}

	run(t, engine, "bin.delete_permanently", map[string]any{"bin_ids": []int64{binID}})

	// The delete step is gone: undo must never bring back something the
	// user meant to destroy.
	if engine.Status().UndoSteps != 0 {
		t.Fatalf("expected no undo steps left, got %d", engine.Status().UndoSteps)
	}
	if _, err := engine.Undo(); !isNothingToUndo(err) {
		t.Errorf("expected nothing to undo, got %v", err)
	}
}

func isNothingToUndo(err error) bool {
	return err != nil && err.Error() == "nothing to undo"
}

func TestFavouriteCreateDeleteRoundTrip(t *testing.T) {
	engine, db := newTestEngine(t)
	_, _ = seedWorkspace(t, db)

	run(t, engine, "favourite.create", map[string]any{"project_id": 1, "name": "my favs"})
	if count(t, db, "SELECT COUNT(*) FROM favourite_collections") != 1 {
		t.Fatalf("expected the favourite collection created")
	}
	if count(t, db, "SELECT COUNT(*) FROM favourite_appearances") != 1 {
		t.Fatalf("expected the default appearance created")
	}

	if _, err := engine.Undo(); err != nil {
		t.Fatalf("undoing create: %v", err)
	}
	if count(t, db, "SELECT COUNT(*) FROM favourite_collections") != 0 {
		t.Fatalf("expected the create undone")
	}
	if count(t, db, "SELECT COUNT(*) FROM favourite_appearances") != 0 {
		t.Fatalf("expected the appearance undone with it")
	}

	// Delete after create is symmetric and undoable.
	created := run(t, engine, "favourite.create", map[string]any{"project_id": 1, "name": "my favs"})
	favourite, ok := created.(map[string]any)
	if !ok {
		t.Fatalf("expected a favourite back, got %T", created)
	}
	favouriteID, _ := favourite["id"].(int64)

	run(t, engine, "favourite.delete", map[string]any{"id": favouriteID})
	if count(t, db, "SELECT COUNT(*) FROM favourite_collections") != 0 {
		t.Fatalf("expected the favourite deleted")
	}
	if _, err := engine.Undo(); err != nil {
		t.Fatalf("undoing delete: %v", err)
	}
	if count(t, db, "SELECT COUNT(*) FROM favourite_collections WHERE id = ?", favouriteID) != 1 {
		t.Fatalf("expected the favourite restored under its original id")
	}
}

func TestEnvironmentAndVariableRoundTrips(t *testing.T) {
	engine, db := newTestEngine(t)
	_, _ = seedWorkspace(t, db)

	run(t, engine, "environment.create", map[string]any{"project_id": 1, "name": "Staging"})
	run(t, engine, "variable.create", map[string]any{"environment_id": 1, "key": "BASE_URL", "value": "http://staging"})

	if count(t, db, "SELECT COUNT(*) FROM environment_variables") != 1 {
		t.Fatalf("expected the variable created")
	}

	// Variable delete is undone with the exact value back.
	run(t, engine, "variable.delete", map[string]any{"id": 1})
	if count(t, db, "SELECT COUNT(*) FROM environment_variables") != 0 {
		t.Fatalf("expected the variable deleted")
	}
	if _, err := engine.Undo(); err != nil {
		t.Fatalf("undoing variable delete: %v", err)
	}
	var value string
	if err := db.QueryRow(
		`SELECT value FROM environment_variables WHERE key = 'BASE_URL'`,
	).Scan(&value); err != nil {
		t.Fatalf("reading the restored variable: %v", err)
	}
	if value != "http://staging" {
		t.Errorf("expected the exact value back, got %q", value)
	}

	// Environment delete refuses the default, undoes for the rest.
	run(t, engine, "environment.create", map[string]any{"project_id": 1, "name": "Default"})
	var stagingID int64
	if err := db.QueryRow(`SELECT id FROM environments WHERE name = 'Staging'`).Scan(&stagingID); err != nil {
		t.Fatalf("reading the staging environment: %v", err)
	}

	input, _ := json.Marshal(map[string]any{"id": stagingID})
	if _, err := engine.Run("environment.delete", input); err != nil {
		t.Fatalf("deleting the staging environment: %v", err)
	}
	if count(t, db, "SELECT COUNT(*) FROM environment_variables") != 0 {
		t.Fatalf("expected the variables deleted with the environment")
	}
	if _, err := engine.Undo(); err != nil {
		t.Fatalf("undoing environment delete: %v", err)
	}
	if count(t, db, "SELECT COUNT(*) FROM environments WHERE id = ?", stagingID) != 1 {
		t.Fatalf("expected the environment restored")
	}
	if count(t, db, "SELECT COUNT(*) FROM environment_variables WHERE environment_id = ?", stagingID) != 1 {
		t.Fatalf("expected the variable restored with it")
	}
}

func TestSaveResponsesWritesStatusAndKeepsHistoryCapped(t *testing.T) {
	engine, db := newTestEngine(t)
	_, _ = seedWorkspace(t, db)
	if _, err := db.Exec(`
		INSERT INTO http_requests (collection_id, name, url, method) VALUES (1, 'r', 'http://x', 'GET')
	`); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	run(t, engine, "response.save_bulk", map[string]any{"responses": []map[string]any{
		{"request_id": 1, "status_code": 200, "body": "one"},
	}})

	var statusCode int
	var responseID int64
	if err := db.QueryRow(
		`SELECT status_code, response_id FROM http_requests WHERE id = 1`,
	).Scan(&statusCode, &responseID); err != nil {
		t.Fatalf("reading the request: %v", err)
	}
	if statusCode != 200 || responseID == 0 {
		t.Fatalf("expected the send's status columns written, got %d/%d", statusCode, responseID)
	}

	// History is capped at ten per request.
	for i := 0; i < 12; i++ {
		run(t, engine, "response.save", map[string]any{
			"request_id": 1, "status_code": 200, "body": "tick",
		})
	}
	if got := count(t, db, "SELECT COUNT(*) FROM responses WHERE request_id = 1"); got != 10 {
		t.Fatalf("expected the history capped at 10, got %d", got)
	}

	// And none of it is undoable.
	if engine.Status().UndoSteps != 0 {
		t.Fatalf("expected no undo steps from response history, got %d", engine.Status().UndoSteps)
	}
}

func TestListCommandsServesTheCatalog(t *testing.T) {
	engine, _ := newTestEngine(t)
	catalog := engine.ListCommands()
	if len(catalog) != len(Catalog) {
		t.Fatalf("expected %d commands, got %d", len(Catalog), len(catalog))
	}
	byName := map[string]bool{}
	for _, info := range catalog {
		byName[info.Name] = true
	}
	for _, name := range []string{
		"request.create", "request.delete", "request.send", "bin.restore",
		"favourite.create", "environment.delete", "curl.parse",
	} {
		if !byName[name] {
			t.Errorf("expected %s in the catalog", name)
		}
	}
}
