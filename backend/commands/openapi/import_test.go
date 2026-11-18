package openapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

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
