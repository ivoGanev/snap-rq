package openapi

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

	item := doc.Paths.Value("/v1/pets/{petId}")
	if item == nil || item.Get == nil {
		t.Fatalf("missing GET /v1/pets/{petId} in %s", string(raw))
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

func exportDoc(t *testing.T, db *sql.DB) (*openapi3.T, map[string]any) {
	t.Helper()
	outPath := filepath.Join(t.TempDir(), "out.json")
	res := runIn(t, db, NewExport, map[string]any{"project_id": 1, "file_path": outPath})
	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading export: %v", err)
	}
	doc, err := (&openapi3.Loader{}).LoadFromData(raw)
	if err != nil {
		t.Fatalf("re-parsing export: %v", err)
	}
	return doc, res
}

func seedRequest(t *testing.T, db *sql.DB, name, method, url, headers string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO http_requests (collection_id, name, url, method, body, body_type, request_headers, user_sort_order)
		 VALUES (1, ?, ?, ?, '', 'json', ?, 0)`, name, url, method, headers,
	); err != nil {
		t.Fatalf("seeding %s: %v", name, err)
	}
}

func TestExportDeclaresPathAndHeaderParameters(t *testing.T) {
	db := openTestDB(t)
	seedRequest(t, db, "getPet", "GET", "https://api.example.com/pets/{{petId}}",
		"X-Trace: secret-token\nAuthorization: Bearer abc\nContent-Type: application/json")

	doc, _ := exportDoc(t, db)
	op := doc.Paths.Value("/pets/{petId}").Get
	got := map[string]string{}
	for _, p := range op.Parameters {
		got[p.Value.Name] = p.Value.In
	}
	if got["petId"] != "path" || got["X-Trace"] != "header" || len(got) != 2 {
		t.Fatalf("parameters = %v, want petId (path) and X-Trace (header) only", got)
	}
	raw, _ := json.Marshal(doc)
	if strings.Contains(string(raw), "secret-token") || strings.Contains(string(raw), "Bearer") {
		t.Fatal("header values must not be written to the file")
	}
}

func TestExportKeepsAVariableBaseURLAsAServerVariable(t *testing.T) {
	db := openTestDB(t)
	seedRequest(t, db, "health", "GET", "{{base_url}}/health", "")

	doc, _ := exportDoc(t, db)
	if doc.Paths.Value("/health") == nil {
		t.Fatalf("missing /health")
	}
	if len(doc.Servers) != 1 || doc.Servers[0].URL != "{base_url}" || doc.Servers[0].Variables["base_url"] == nil {
		t.Fatalf("servers = %+v, want one {base_url} server with a variable", doc.Servers)
	}
}

func TestExportSkipsDuplicateOperationsAndKeepsIDsUnique(t *testing.T) {
	db := openTestDB(t)
	seedRequest(t, db, "list", "GET", "https://a.test/items", "")
	seedRequest(t, db, "list again", "GET", "https://a.test/items?page=2", "")
	seedRequest(t, db, "list", "GET", "https://a.test/other", "")
	seedRequest(t, db, "odd", "BREW", "https://a.test/coffee", "")

	doc, res := exportDoc(t, db)
	if res["requests_exported"] != 2 || res["requests_skipped"] != 2 {
		t.Fatalf("result = %v, want 2 exported and 2 skipped", res)
	}
	a, b := doc.Paths.Value("/items").Get.OperationID, doc.Paths.Value("/other").Get.OperationID
	if a == b {
		t.Fatalf("operation ids collide: %q and %q", a, b)
	}
}

func TestExportThenImportGivesBackTheSameRequests(t *testing.T) {
	source := openTestDB(t)
	runIn(t, source, NewImport, map[string]any{
		"project_id": 1, "file_path": writeSpec(t, "spec.json", sampleSpec),
	})

	outPath := filepath.Join(t.TempDir(), "out.json")
	runIn(t, source, NewExport, map[string]any{"project_id": 1, "file_path": outPath})

	target := openTestDB(t)
	runIn(t, target, NewImport, map[string]any{"project_id": 1, "file_path": outPath})

	rows := func(db *sql.DB) []string {
		r, err := db.Query(`SELECT c.name || ' | ' || x.method || ' ' || x.url || ' | ' || x.body
		                      FROM http_requests x JOIN collections c ON c.id = x.collection_id
		                     ORDER BY 1`)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		defer r.Close()
		var out []string
		for r.Next() {
			var line string
			r.Scan(&line)
			out = append(out, line)
		}
		return out
	}
	want, got := rows(source), rows(target)
	if len(want) != 4 || strings.Join(want, "\n") != strings.Join(got, "\n") {
		t.Fatalf("round trip changed the requests:\nbefore %q\nafter  %q", want, got)
	}
}
