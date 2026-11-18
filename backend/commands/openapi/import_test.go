package openapi

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"snap-rq/backend/commands"
	"snap-rq/backend/database"
)

func writeSpec(t *testing.T, name, spec string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(spec), 0o644); err != nil {
		t.Fatalf("writing spec: %v", err)
	}
	return path
}

func TestImportCreatesCollectionsAndRequests(t *testing.T) {
	db, err := database.OpenPath(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	defer db.Close()

	specPath := writeSpec(t, "petstore.json", sampleSpec)

	payload, _ := json.Marshal(map[string]any{"project_id": 1, "file_path": specPath})

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("beginning tx: %v", err)
	}
	res, err := NewImport(tx).Run(json.RawMessage(payload))
	if err != nil {
		tx.Rollback()
		t.Fatalf("import: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("committing: %v", err)
	}

	counts := res.(map[string]any)
	if counts["collections_created"] != 3 || counts["requests_created"] != 4 {
		t.Fatalf("counts = %v, want 3 collections and 4 requests", counts)
	}

	var requests int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM http_requests WHERE collection_id IN (SELECT id FROM collections WHERE project_id = 1)`,
	).Scan(&requests); err != nil {
		t.Fatalf("counting requests: %v", err)
	}
	if requests != 4 {
		t.Fatalf("expected 4 requests, got %d", requests)
	}

	var url, method, bodyType, collection string
	if err := db.QueryRow(
		`SELECT r.url, r.method, r.body_type, c.name
		   FROM http_requests r JOIN collections c ON c.id = r.collection_id
		  WHERE r.name = 'createPet'`,
	).Scan(&url, &method, &bodyType, &collection); err != nil {
		t.Fatalf("reading createPet: %v", err)
	}
	if collection != "pets" || method != "POST" || bodyType != "json" {
		t.Fatalf("createPet = col %q method %q type %q", collection, method, bodyType)
	}
	if url != "https://api.example.com/v1/pets/{{petId}}" {
		t.Fatalf("createPet url = %q", url)
	}
}

func TestImportIsNotUndoable(t *testing.T) {
	if _, ok := any(NewImport(nil)).(interface {
		Undo(any, json.RawMessage) error
	}); ok {
		t.Fatal("import must not implement Undo")
	}
}

func TestImportRejectsSpecWithoutPaths(t *testing.T) {
	db, err := database.OpenPath(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	defer db.Close()

	specPath := writeSpec(t, "empty.json", `{"openapi":"3.0.0","info":{"title":"Empty","version":"1"},"paths":{}}`)
	payload, _ := json.Marshal(map[string]any{"project_id": 1, "file_path": specPath})

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("beginning tx: %v", err)
	}
	defer tx.Rollback()
	if _, err := NewImport(tx).Run(json.RawMessage(payload)); err == nil {
		t.Fatal("expected an error for a spec with no paths")
	}
}

// runIn runs a command inside a transaction and commits it.
func runIn(t *testing.T, db *sql.DB, newCmd func(*sql.Tx) commands.Command, input map[string]any) map[string]any {
	t.Helper()
	payload, _ := json.Marshal(input)
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("beginning tx: %v", err)
	}
	res, err := newCmd(tx).Run(json.RawMessage(payload))
	if err != nil {
		tx.Rollback()
		t.Fatalf("running command: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("committing: %v", err)
	}
	return res.(map[string]any)
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := database.OpenPath(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestImportTwiceCreatesNothingTheSecondTime(t *testing.T) {
	db := openTestDB(t)
	specPath := writeSpec(t, "spec.json", sampleSpec)
	in := map[string]any{"project_id": 1, "file_path": specPath}

	first := runIn(t, db, NewImport, in)
	if first["collections_created"] != 3 {
		t.Fatalf("first import = %v, want 3 collections", first)
	}
	second := runIn(t, db, NewImport, in)
	if second["collections_created"] != 0 || second["requests_created"] != 0 || second["collections_skipped"] != 3 {
		t.Fatalf("second import = %v, want nothing created and 3 skipped", second)
	}
}

func TestImportOnlyTheSelectedCollections(t *testing.T) {
	db := openTestDB(t)
	specPath := writeSpec(t, "spec.json", sampleSpec)

	res := runIn(t, db, NewImport, map[string]any{
		"project_id": 1, "file_path": specPath, "collections": []string{"STORE"},
	})
	if res["collections_created"] != 1 || res["requests_created"] != 1 {
		t.Fatalf("selected import = %v, want 1 collection with 1 request", res)
	}

	none := runIn(t, db, NewImport, map[string]any{
		"project_id": 1, "file_path": specPath, "collections": []string{},
	})
	if none["collections_created"] != 0 {
		t.Fatalf("empty selection imported %v", none)
	}
}

func TestPreviewMarksExistingCollectionsAndWritesNothing(t *testing.T) {
	db := openTestDB(t)
	specPath := writeSpec(t, "spec.json", sampleSpec)
	runIn(t, db, NewImport, map[string]any{
		"project_id": 1, "file_path": specPath, "collections": []string{"pets"},
	})

	var before int
	db.QueryRow(`SELECT COUNT(*) FROM collections`).Scan(&before)

	res := runIn(t, db, NewPreview, map[string]any{"project_id": 1, "file_path": specPath})
	cols := res["collections"].([]previewCollection)
	if len(cols) != 3 {
		t.Fatalf("preview listed %d collections, want 3", len(cols))
	}
	for _, c := range cols {
		if want := c.Name == "pets"; c.Exists != want {
			t.Fatalf("%q exists = %v, want %v", c.Name, c.Exists, want)
		}
	}

	var after int
	db.QueryRow(`SELECT COUNT(*) FROM collections`).Scan(&after)
	if after != before {
		t.Fatalf("preview changed the collection count from %d to %d", before, after)
	}
}
