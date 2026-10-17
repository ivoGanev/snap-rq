package services

import "testing"

func TestBulkUpdateStringFields(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	runMigrations(t, db)

	svc := NewRequestService(db)

	res, err := db.Exec("INSERT INTO profiles (name) VALUES (?)", "p")
	if err != nil {
		t.Fatalf("insert profile: %v", err)
	}
	profileID, _ := res.LastInsertId()
	res, err = db.Exec("INSERT INTO projects (profile_id) VALUES (?)", profileID)
	if err != nil {
		t.Fatalf("insert project: %v", err)
	}
	projectID, _ := res.LastInsertId()
	res, err = db.Exec("INSERT INTO collections (project_id, name) VALUES (?, ?)", projectID, "c1")
	if err != nil {
		t.Fatalf("insert collection: %v", err)
	}
	collectionID, _ := res.LastInsertId()

	var ids []int64
	for i := 0; i < 2; i++ {
		res, err := db.Exec(`
			INSERT INTO http_requests (collection_id, name, url, method, body, request_headers, status_code, response_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			collectionID, "req", "https://example.com", "GET", "body", "headers", 200, 0)
		if err != nil {
			t.Fatalf("insert request: %v", err)
		}
		id, _ := res.LastInsertId()
		ids = append(ids, id)
	}

	updated, err := svc.BulkUpdateRequestName(ids, "renamed")
	if err != nil {
		t.Fatalf("BulkUpdateRequestName failed: %v", err)
	}
	if len(updated) != 2 {
		t.Fatalf("expected 2 updated requests, got %d", len(updated))
	}
	for i, req := range updated {
		if req.Name != "renamed" {
			t.Fatalf("updated[%d] name = %q, want renamed", i, req.Name)
		}
	}

	updated, err = svc.BulkUpdateRequestUrl(ids, "https://new.example.com")
	if err != nil {
		t.Fatalf("BulkUpdateRequestUrl failed: %v", err)
	}
	for i, req := range updated {
		if req.URL != "https://new.example.com" {
			t.Fatalf("updated[%d] url = %q", i, req.URL)
		}
	}

	updated, err = svc.BulkUpdateRequestMethod(ids, "POST")
	if err != nil {
		t.Fatalf("BulkUpdateRequestMethod failed: %v", err)
	}
	for i, req := range updated {
		if req.Method != "POST" {
			t.Fatalf("updated[%d] method = %q", i, req.Method)
		}
	}

	// Values should be persisted.
	for _, id := range ids {
		var name, url, method string
		if err := db.QueryRow(
			"SELECT name, url, method FROM http_requests WHERE id = ?", id,
		).Scan(&name, &url, &method); err != nil {
			t.Fatalf("reading request %d: %v", id, err)
		}
		if name != "renamed" || url != "https://new.example.com" || method != "POST" {
			t.Fatalf("request %d not updated: name=%q url=%q method=%q", id, name, url, method)
		}
	}

	// Empty id list is a no-op.
	empty, err := svc.BulkUpdateRequestName(nil, "ignored")
	if err != nil {
		t.Fatalf("BulkUpdateRequestName(nil) failed: %v", err)
	}
	if empty != nil {
		t.Fatalf("expected nil result for empty ids, got %v", empty)
	}
}
