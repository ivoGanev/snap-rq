package services

import (
	"testing"

	"snap-rq/backend/models"
)

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

	const headersValue = "Authorization: Bearer token\nX-Test: 1"
	updated, err = svc.BulkUpdateRequestHeaders(ids, headersValue)
	if err != nil {
		t.Fatalf("BulkUpdateRequestHeaders failed: %v", err)
	}
	for i, req := range updated {
		if req.RequestHeaders != headersValue {
			t.Fatalf("updated[%d] request_headers = %q", i, req.RequestHeaders)
		}
	}

	// Values should be persisted.
	for _, id := range ids {
		var name, url, method, headers string
		if err := db.QueryRow(
			"SELECT name, url, method, request_headers FROM http_requests WHERE id = ?", id,
		).Scan(&name, &url, &method, &headers); err != nil {
			t.Fatalf("reading request %d: %v", id, err)
		}
		if name != "renamed" || url != "https://new.example.com" || method != "POST" ||
			headers != headersValue {
			t.Fatalf(
				"request %d not updated: name=%q url=%q method=%q headers=%q",
				id, name, url, method, headers,
			)
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

func TestBulkSaveResponses(t *testing.T) {
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
			collectionID, "req", "https://example.com", "GET", "", "", 0, 0)
		if err != nil {
			t.Fatalf("insert request: %v", err)
		}
		id, _ := res.LastInsertId()
		ids = append(ids, id)
	}

	saved, err := svc.BulkSaveResponses([]models.HttpResponse{
		{RequestID: ids[0], StatusCode: 200, Headers: "H: 1", Body: "one", DurationMs: 11},
		{RequestID: ids[1], StatusCode: 201, Headers: "H: 2", Body: "two", DurationMs: 22},
	})
	if err != nil {
		t.Fatalf("BulkSaveResponses failed: %v", err)
	}
	if len(saved) != 2 {
		t.Fatalf("expected 2 saved responses, got %d", len(saved))
	}

	for i, resp := range saved {
		if resp.ID == 0 {
			t.Fatalf("saved[%d] has no id", i)
		}
		if resp.CreatedAt == "" {
			t.Fatalf("saved[%d] has no created_at", i)
		}

		var statusCode int
		var body string
		if err := db.QueryRow(
			"SELECT status_code, body FROM responses WHERE id = ?", resp.ID,
		).Scan(&statusCode, &body); err != nil {
			t.Fatalf("reading response %d: %v", resp.ID, err)
		}
		if statusCode != resp.StatusCode || body != resp.Body {
			t.Fatalf("response %d not persisted: status=%d body=%q", resp.ID, statusCode, body)
		}

		var requestStatus int
		var responseID int64
		if err := db.QueryRow(
			"SELECT status_code, response_id FROM http_requests WHERE id = ?", resp.RequestID,
		).Scan(&requestStatus, &responseID); err != nil {
			t.Fatalf("reading request %d: %v", resp.RequestID, err)
		}
		if requestStatus != resp.StatusCode || responseID != resp.ID {
			t.Fatalf(
				"request %d not updated: status=%d response_id=%d",
				resp.RequestID, requestStatus, responseID,
			)
		}
	}

	empty, err := svc.BulkSaveResponses(nil)
	if err != nil {
		t.Fatalf("BulkSaveResponses(nil) failed: %v", err)
	}
	if empty != nil {
		t.Fatalf("expected nil result for empty responses, got %v", empty)
	}
}
