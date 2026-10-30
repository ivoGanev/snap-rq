package services

import (
	"database/sql"
	"encoding/json"
	"testing"

	"snap-rq/backend/commands"
)

// newCommandTestHistory registers the full command list against a fresh
// history, the way the app does at startup, so tests can run mutations the
// way the app does: by command name.
func newCommandTestHistory(t *testing.T, db *sql.DB) *commands.History {
	t.Helper()
	history := commands.NewHistory(db)
	err := RegisterCommands(history,
		NewRequestService(db),
		NewFavouriteService(db),
		NewEnvironmentService(db),
		NewEnvironmentVariableService(db),
	)
	if err != nil {
		t.Fatalf("registering commands: %v", err)
	}
	return history
}

func runCommand(t *testing.T, history *commands.History, name string, payload any) any {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encoding payload: %v", err)
	}
	out, err := history.Run(name, raw)
	if err != nil {
		t.Fatalf("running %s: %v", name, err)
	}
	return out
}

// createEnvironmentTables adds the environments tables the shared test
// schema lacks, mirroring production including the FK cascade.
func createEnvironmentTables(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`
		CREATE TABLE environments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE environment_variables (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			environment_id INTEGER NOT NULL,
			key TEXT NOT NULL,
			value TEXT NOT NULL,
			FOREIGN KEY (environment_id) REFERENCES environments(id) ON DELETE CASCADE
		);
	`); err != nil {
		t.Fatalf("creating environments schema: %v", err)
	}
}

// createFavouriteAppearanceTable adds the favourite appearance table the
// shared test schema lacks, mirroring production including the FK cascade.
func createFavouriteAppearanceTable(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`
		CREATE TABLE favourite_appearances (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			favourite_collection_id INTEGER NOT NULL,
			appearance_type TEXT NOT NULL,
			appearance_value TEXT NOT NULL,
			FOREIGN KEY (favourite_collection_id) REFERENCES favourite_collections(id) ON DELETE CASCADE
		);
	`); err != nil {
		t.Fatalf("creating favourite appearances schema: %v", err)
	}
}

func TestDeleteRequestViaHistoryIsUndoable(t *testing.T) {
	db := newTestDB(t)
	runMigrations(t, db)
	history := newCommandTestHistory(t, db)

	if _, err := db.Exec(`
		INSERT INTO profiles (name) VALUES ('p');
		INSERT INTO projects (profile_id) VALUES (1);
		INSERT INTO collections (project_id, name) VALUES (1, 'col');
		INSERT INTO http_requests (collection_id, name, url, method, response_id) VALUES (1, 'victim', 'http://x', 'GET', 1);
		INSERT INTO responses (request_id, status_code, body) VALUES (1, 200, 'payload');
		INSERT INTO tags (project_id, name) VALUES (1, 'red');
		INSERT INTO request_tags (request_id, tag_id) VALUES (1, 1);
		INSERT INTO favourite_collections (project_id, name) VALUES (1, 'fav');
		INSERT INTO favourite_items (favourite_collection_id, http_request_id) VALUES (1, 1);
	`); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	runCommand(t, history, RequestDeleteCommandName, map[string]int64{"id": 1})

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM http_requests").Scan(&count); err != nil {
		t.Fatalf("counting requests: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected request binned")
	}
	var binCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM binned_requests").Scan(&binCount); err != nil {
		t.Fatalf("counting binned requests: %v", err)
	}
	if binCount != 1 {
		t.Fatalf("expected one binned request, got %d", binCount)
	}

	if _, err := history.Undo(); err != nil {
		t.Fatalf("undoing delete: %v", err)
	}

	// The request is back under its original id, with its response, tag
	// membership and favourite membership intact.
	var name string
	var responseID, tagCount, favCount int
	if err := db.QueryRow("SELECT name, response_id FROM http_requests WHERE id = 1").Scan(&name, &responseID); err != nil {
		t.Fatalf("reading restored request: %v", err)
	}
	if name != "victim" || responseID != 1 {
		t.Errorf("expected restored request with original response link, got name=%q response_id=%d", name, responseID)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM request_tags WHERE request_id = 1").Scan(&tagCount); err != nil {
		t.Fatalf("counting tag memberships: %v", err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM favourite_items WHERE http_request_id = 1").Scan(&favCount); err != nil {
		t.Fatalf("counting favourite memberships: %v", err)
	}
	if tagCount != 1 || favCount != 1 {
		t.Errorf("expected restored memberships, got tags=%d favourites=%d", tagCount, favCount)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM binned_requests").Scan(&binCount); err != nil {
		t.Fatalf("counting binned requests after undo: %v", err)
	}
	if binCount != 0 {
		t.Errorf("expected bin row removed by undo, got %d", binCount)
	}
}

func TestDeleteFavouriteCollectionViaHistoryIsUndoable(t *testing.T) {
	db := newTestDB(t)
	runMigrations(t, db)
	createFavouriteAppearanceTable(t, db)
	history := newCommandTestHistory(t, db)

	if _, err := db.Exec(`
		INSERT INTO profiles (name) VALUES ('p');
		INSERT INTO projects (profile_id) VALUES (1);
		INSERT INTO collections (project_id, name) VALUES (1, 'col');
		INSERT INTO http_requests (collection_id, name, url, method) VALUES (1, 'r', 'http://x', 'GET');
		INSERT INTO favourite_collections (project_id, name) VALUES (1, 'fav');
		INSERT INTO favourite_appearances (favourite_collection_id, appearance_type, appearance_value) VALUES (1, 'icon', 'ico_1a9d4');
		INSERT INTO favourite_items (favourite_collection_id, http_request_id) VALUES (1, 1);
	`); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	runCommand(t, history, FavouriteDeleteCollectionCommandName, map[string]int64{"id": 1})

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM favourite_collections").Scan(&count); err != nil {
		t.Fatalf("counting favourite collections: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected favourite collection deleted")
	}

	if _, err := history.Undo(); err != nil {
		t.Fatalf("undoing delete: %v", err)
	}

	var itemCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM favourite_items WHERE favourite_collection_id = 1").Scan(&itemCount); err != nil {
		t.Fatalf("counting favourite items: %v", err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM favourite_collections WHERE id = 1").Scan(&count); err != nil {
		t.Fatalf("reading restored favourite collection: %v", err)
	}
	if count != 1 || itemCount != 1 {
		t.Errorf("expected restored collection with memberships, got collections=%d items=%d", count, itemCount)
	}
}

func TestDeleteEnvironmentViaHistoryIsUndoable(t *testing.T) {
	db := newTestDB(t)
	runMigrations(t, db)
	createEnvironmentTables(t, db)
	history := newCommandTestHistory(t, db)

	if _, err := db.Exec(`
		INSERT INTO profiles (name) VALUES ('p');
		INSERT INTO projects (profile_id) VALUES (1);
		INSERT INTO environments (project_id, name) VALUES (1, 'Default');
		INSERT INTO environments (project_id, name) VALUES (1, 'Staging');
		INSERT INTO environment_variables (environment_id, key, value) VALUES (2, 'BASE_URL', 'http://staging');
	`); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	// The default environment is still refused through the command path.
	if _, err := history.Run(EnvironmentDeleteCommandName, json.RawMessage(`{"id":1}`)); err == nil {
		t.Fatalf("expected default environment delete to be refused")
	}

	runCommand(t, history, EnvironmentDeleteCommandName, map[string]int64{"id": 2})

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM environments WHERE id = 2").Scan(&count); err != nil {
		t.Fatalf("counting environments: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected environment deleted")
	}

	if _, err := history.Undo(); err != nil {
		t.Fatalf("undoing delete: %v", err)
	}

	var value string
	if err := db.QueryRow(`
		SELECT ev.value FROM environments e
		JOIN environment_variables ev ON ev.environment_id = e.id
		WHERE e.id = 2 AND ev.key = 'BASE_URL'
	`).Scan(&value); err != nil {
		t.Fatalf("reading restored variable: %v", err)
	}
	if value != "http://staging" {
		t.Errorf("expected restored variable value, got %q", value)
	}
}

func TestDeleteVariableViaHistoryIsUndoable(t *testing.T) {
	db := newTestDB(t)
	runMigrations(t, db)
	createEnvironmentTables(t, db)
	history := newCommandTestHistory(t, db)

	if _, err := db.Exec(`
		INSERT INTO profiles (name) VALUES ('p');
		INSERT INTO projects (profile_id) VALUES (1);
		INSERT INTO environments (project_id, name) VALUES (1, 'Default');
		INSERT INTO environment_variables (environment_id, key, value) VALUES (1, 'API_KEY', 'secret-1');
		INSERT INTO environment_variables (environment_id, key, value) VALUES (1, 'BASE_URL', 'http://x');
	`); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	runCommand(t, history, EnvironmentVariableDeleteCommandName, map[string]int64{"id": 1})

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM environment_variables WHERE id = 1").Scan(&count); err != nil {
		t.Fatalf("counting variables: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected variable deleted")
	}

	if _, err := history.Undo(); err != nil {
		t.Fatalf("undoing delete: %v", err)
	}

	var value string
	if err := db.QueryRow("SELECT value FROM environment_variables WHERE id = 1").Scan(&value); err != nil {
		t.Fatalf("reading restored variable: %v", err)
	}
	if value != "secret-1" {
		t.Errorf("expected restored variable value, got %q", value)
	}
}

func TestPermanentDeleteForgetsUndoSteps(t *testing.T) {
	db := newTestDB(t)
	runMigrations(t, db)

	// The permanent delete goes through the same service instance whose
	// commands were registered, the way the app wires it.
	requests := NewRequestService(db)
	history := commands.NewHistory(db)
	err := RegisterCommands(history,
		requests, NewFavouriteService(db), NewEnvironmentService(db), NewEnvironmentVariableService(db))
	if err != nil {
		t.Fatalf("registering commands: %v", err)
	}

	if _, err := db.Exec(`
		INSERT INTO profiles (name) VALUES ('p');
		INSERT INTO projects (profile_id) VALUES (1);
		INSERT INTO collections (project_id, name) VALUES (1, 'col');
		INSERT INTO http_requests (collection_id, name, url, method) VALUES (1, 'victim', 'http://x', 'GET');
	`); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	// Delete a request, then permanently destroy the bin row.
	runCommand(t, history, RequestDeleteCommandName, map[string]int64{"id": 1})
	if _, err := history.Undo(); err != nil {
		t.Fatalf("undoing delete: %v", err)
	}
	runCommand(t, history, RequestDeleteCommandName, map[string]int64{"id": 1})

	var binID int64
	if err := db.QueryRow("SELECT id FROM binned_requests").Scan(&binID); err != nil {
		t.Fatalf("reading bin row: %v", err)
	}
	if err := requests.PermanentlyDeleteBinnedRequests([]int64{binID}); err != nil {
		t.Fatalf("permanently deleting: %v", err)
	}

	// The delete step and its redo step are gone: undo must never bring
	// back something the user meant to destroy.
	status := history.Status()
	if status.UndoSteps != 0 {
		t.Errorf("expected no undo steps left, got %d", status.UndoSteps)
	}
	if _, err := history.Redo(); err == nil {
		t.Errorf("expected redo to be impossible")
	}
}
