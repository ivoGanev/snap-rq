package openapi

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"snap-rq/backend/database"
)

// The vendored specs under testdata/ are real OpenAPI documents fetched from
// the internet (Swagger Petstore v3). They are committed so tests never hit
// the network.
func importFile(t *testing.T, specPath string) map[string]any {
	t.Helper()
	db, err := database.OpenPath(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	defer db.Close()

	abs, err := filepath.Abs(specPath)
	if err != nil {
		t.Fatalf("resolving path: %v", err)
	}
	payload, _ := json.Marshal(map[string]any{"project_id": 1, "file_path": abs})

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("beginning tx: %v", err)
	}
	res, err := NewImport(tx).Run(json.RawMessage(payload))
	if err != nil {
		tx.Rollback()
		t.Fatalf("import %s: %v", specPath, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("committing: %v", err)
	}

	counts := res.(map[string]any)
	if counts["collections_created"] != 3 || counts["requests_created"] != 19 {
		t.Fatalf("%s: counts = %v, want 3 collections and 19 requests", specPath, counts)
	}

	spotCheck := func(name, wantURL, wantMethod, wantType, wantCol string) {
		t.Helper()
		var url, method, bodyType, collection string
		if err := db.QueryRow(
			`SELECT r.url, r.method, r.body_type, c.name
			   FROM http_requests r JOIN collections c ON c.id = r.collection_id
			  WHERE r.name = ?`, name,
		).Scan(&url, &method, &bodyType, &collection); err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		if url != wantURL || method != wantMethod || bodyType != wantType || collection != wantCol {
			t.Fatalf("%s = url %q method %q type %q col %q", name, url, method, bodyType, collection)
		}
	}

	// Petstore declares a relative server ("/api/v3"), so imported URLs keep
	// that base verbatim.
	spotCheck(
		"addPet",
		"/api/v3/pet",
		"POST", "json", "pet",
	)
	spotCheck(
		"getPetById",
		"/api/v3/pet/{{petId}}",
		"GET", "json", "pet",
	)

	return counts
}

func TestImportPetstoreJSON(t *testing.T) {
	importFile(t, filepath.Join("testdata", "petstore.json"))
}

func TestImportPetstoreYAML(t *testing.T) {
	importFile(t, filepath.Join("testdata", "petstore.yaml"))
}
