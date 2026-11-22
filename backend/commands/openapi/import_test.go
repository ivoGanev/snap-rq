package openapi

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
	if first["collections_created"] != 3 || first["requests_created"] != 4 {
		t.Fatalf("first import = %v, want 3 collections and 4 requests", first)
	}
	second := runIn(t, db, NewImport, in)
	if second["collections_created"] != 0 || second["requests_created"] != 0 || second["requests_updated"] != 0 {
		t.Fatalf("second import = %v, want nothing created or updated", second)
	}
}

// Keys include the server's base path (/v1 here), so take them from the
// preview, as the import dialog does.
func previewKey(t *testing.T, db *sql.DB, specPath, collection, method, path string) string {
	t.Helper()
	pv := runIn(t, db, NewPreview, map[string]any{"project_id": 1, "file_path": specPath})
	for _, c := range pv["collections"].([]previewCollection) {
		if !strings.EqualFold(c.Name, collection) {
			continue
		}
		for _, r := range c.Requests {
			if r.Method == method && strings.HasSuffix(r.Key, path) {
				return r.Key
			}
		}
	}
	t.Fatalf("no %s %s in the preview's %q collection", method, path, collection)
	return ""
}

func TestImportOnlyWhatWasPicked(t *testing.T) {
	db := openTestDB(t)
	specPath := writeSpec(t, "spec.json", sampleSpec)
	orders := previewKey(t, db, specPath, "store", "GET", "/store/orders")

	res := runIn(t, db, NewImport, map[string]any{
		"project_id": 1, "file_path": specPath,
		"collections": []map[string]any{{"name": "STORE", "add": []string{orders}}},
	})
	if res["collections_created"] != 1 || res["requests_created"] != 1 {
		t.Fatalf("picked import = %v, want 1 collection with 1 request", res)
	}

	none := runIn(t, db, NewImport, map[string]any{
		"project_id": 1, "file_path": specPath, "collections": []map[string]any{},
	})
	if none["collections_created"] != 0 || none["requests_created"] != 0 {
		t.Fatalf("empty pick imported %v", none)
	}
}

func TestImportAddsNewRequestsToAnExistingCollectionAndUpdatesPickedOnes(t *testing.T) {
	db := openTestDB(t)
	runIn(t, db, NewImport, map[string]any{
		"project_id": 1, "file_path": writeSpec(t, "v1.json", sampleSpec),
	})

	// A new version of the spec: one more operation in "store", and the
	// existing order listing now takes a "status" query parameter.
	v2 := strings.Replace(sampleSpec, `"/store/orders": {
      "get": {"operationId": "listOrders", "tags": ["store"]}
    },`, `"/store/orders": {
      "get": {"operationId": "listOrders", "tags": ["store"],
        "parameters": [{"name": "status", "in": "query", "schema": {"type": "string"}}]}
    },
    "/store/returns": {
      "post": {"operationId": "createReturn", "tags": ["store"]}
    },`, 1)
	if v2 == sampleSpec {
		t.Fatal("test spec was not changed")
	}
	v2Path := writeSpec(t, "v2.json", v2)

	pv := runIn(t, db, NewPreview, map[string]any{"project_id": 1, "file_path": v2Path})
	var store previewCollection
	for _, c := range pv["collections"].([]previewCollection) {
		if c.Name == "store" {
			store = c
		}
	}
	if !store.Exists || store.New != 1 || store.Changed != 1 || store.Unchanged != 0 {
		t.Fatalf("store preview = %+v, want exists, 1 new, 1 changed", store)
	}

	// Adding only: the changed request stays as it was.
	returns := previewKey(t, db, v2Path, "store", "POST", "/store/returns")
	orders := previewKey(t, db, v2Path, "store", "GET", "/store/orders")
	res := runIn(t, db, NewImport, map[string]any{
		"project_id": 1, "file_path": v2Path,
		"collections": []map[string]any{{"name": "store", "add": []string{returns}}},
	})
	if res["collections_created"] != 0 || res["requests_created"] != 1 || res["requests_updated"] != 0 {
		t.Fatalf("add-only import = %v", res)
	}
	var url string
	db.QueryRow(`SELECT url FROM http_requests WHERE name = 'listOrders'`).Scan(&url)
	if strings.Contains(url, "status") {
		t.Fatalf("listOrders changed without being picked: %q", url)
	}

	// Now pick the update.
	res = runIn(t, db, NewImport, map[string]any{
		"project_id": 1, "file_path": v2Path,
		"collections": []map[string]any{{"name": "store", "update": []string{orders}}},
	})
	if res["requests_updated"] != 1 {
		t.Fatalf("update import = %v, want 1 updated", res)
	}
	db.QueryRow(`SELECT url FROM http_requests WHERE name = 'listOrders'`).Scan(&url)
	if url != "https://api.example.com/v1/store/orders?status={{status}}" {
		t.Fatalf("listOrders url = %q", url)
	}
}

func TestPreviewWritesNothing(t *testing.T) {
	db := openTestDB(t)
	specPath := writeSpec(t, "spec.json", sampleSpec)

	var before int
	db.QueryRow(`SELECT COUNT(*) FROM collections`).Scan(&before)

	res := runIn(t, db, NewPreview, map[string]any{"project_id": 1, "file_path": specPath})
	cols := res["collections"].([]previewCollection)
	if len(cols) != 3 {
		t.Fatalf("preview listed %d collections, want 3", len(cols))
	}
	for _, c := range cols {
		if c.Exists || c.New == 0 {
			t.Fatalf("%q = %+v, want a new collection with new requests", c.Name, c)
		}
	}

	var after, requests int
	db.QueryRow(`SELECT COUNT(*) FROM collections`).Scan(&after)
	db.QueryRow(`SELECT COUNT(*) FROM http_requests`).Scan(&requests)
	if after != before || requests != 0 {
		t.Fatalf("preview wrote data: collections %d to %d, requests %d", before, after, requests)
	}
}
