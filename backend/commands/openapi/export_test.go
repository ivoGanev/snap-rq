package openapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"snap-rq/backend/database"
)

func TestExportWritesAnOpenApiFile(t *testing.T) {
	db, err := database.OpenPath(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	defer db.Close()

	// The default project (id 1) owns a default collection "my stuff" (id 1).
	if _, err := db.Exec(
		`INSERT INTO http_requests (collection_id, name, url, method, body, body_type, request_headers, user_sort_order)
		 VALUES (1, 'getPet', 'https://api.example.com/v1/pets/{{petId}}?verbose=true', 'GET', '', 'json', '', 0)`,
	); err != nil {
		t.Fatalf("seeding request: %v", err)
	}

	outPath := filepath.Join(t.TempDir(), "out.json")

	payload, _ := json.Marshal(map[string]any{"project_id": 1, "file_path": outPath})

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("beginning tx: %v", err)
	}
	res, err := NewExport(tx).Run(json.RawMessage(payload))
	if err != nil {
		tx.Rollback()
		t.Fatalf("export: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("committing: %v", err)
	}

	counts := res.(map[string]any)
	if counts["requests_exported"] != 1 {
		t.Fatalf("counts = %v, want 1 request exported", counts)
	}

	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading export: %v", err)
	}

	loader := openapi3.Loader{}
	doc, err := loader.LoadFromData(raw)
	if err != nil {
		t.Fatalf("re-parsing export: %v", err)
	}

	if doc.Info == nil || doc.Info.Title != "Default Project" {
		t.Fatalf("info = %+v", doc.Info)
	}

	item := doc.Paths.Value("/v1/pets/{{petId}}")
	if item == nil || item.Get == nil {
		t.Fatalf("missing GET /v1/pets/{{petId}} in %s", string(raw))
	}
	if len(item.Get.Tags) != 1 || item.Get.Tags[0] != "my stuff" {
		t.Fatalf("tags = %v", item.Get.Tags)
	}

	foundServer := false
	for _, s := range doc.Servers {
		if s.URL == "https://api.example.com" {
			foundServer = true
		}
	}
	if !foundServer {
		t.Fatalf("servers = %+v, missing https://api.example.com", doc.Servers)
	}
}
