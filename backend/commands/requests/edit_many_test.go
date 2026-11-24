package requests_test

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"snap-rq/backend/models"
)

func (h *harness) runErr(name string, in any) error {
	h.t.Helper()
	raw, _ := json.Marshal(in)
	_, err := h.runner.Run(name, raw)
	return err
}

func (h *harness) get(id int64) models.HttpRequest {
	h.t.Helper()
	var req models.HttpRequest
	h.run("request.get", map[string]any{"id": id}, &req)
	return req
}

func TestEditManyAppliesEveryChangeAndReturnsTheRequests(t *testing.T) {
	h := newHarness(t)
	a := h.create(models.HttpRequest{Name: "a", Method: "GET", URL: "http://a"})
	b := h.create(models.HttpRequest{Name: "b", Method: "GET", URL: "http://b"})

	var updated []models.HttpRequest
	h.run("request.edit_many", map[string]any{"changes": []map[string]any{
		{"id": a, "field": "body", "value": `{"networkDeviceId": "dev-exc-1", "priority": 5}`},
		{"id": b, "field": "request_headers", "value": "X-Team: {{team}}"},
		{"id": a, "field": "method", "value": "post"},
	}}, &updated)

	if len(updated) != 2 || updated[0].ID != a || updated[1].ID != b {
		t.Fatalf("returned %+v, want requests %d and %d once each", updated, a, b)
	}
	if updated[0].Body != `{"networkDeviceId": "dev-exc-1", "priority": 5}` || updated[0].Method != "POST" {
		t.Errorf("a = %+v", updated[0])
	}
	if updated[1].RequestHeaders != "X-Team: {{team}}" {
		t.Errorf("b = %+v", updated[1])
	}
}

func TestEditManyChangesNothingWhenOneChangeFails(t *testing.T) {
	h := newHarness(t)
	a := h.create(models.HttpRequest{Name: "a", Method: "GET", URL: "http://a"})
	b := h.create(models.HttpRequest{Name: "b", Method: "GET", URL: "http://b"})

	err := h.runErr("request.edit_many", map[string]any{"changes": []map[string]any{
		{"id": a, "field": "url", "value": "http://changed"},
		{"id": b, "field": "body", "value": map[string]any{"priority": 5}},
	}})
	if err == nil || !strings.Contains(err.Error(), "body must be a string, not an object") ||
		!strings.Contains(err.Error(), "request "+strconv.FormatInt(b, 10)) {
		t.Fatalf("err = %v, want it to name request %d and say body must be a string", err, b)
	}
	if got := h.get(a).URL; got != "http://a" {
		t.Errorf("request %d's url is %q: a failed call must change nothing", a, got)
	}

	err = h.runErr("request.edit_many", map[string]any{"changes": []map[string]any{
		{"id": a, "field": "url", "value": "http://changed"},
		{"id": 99999, "field": "url", "value": "http://x"},
	}})
	if err == nil || !strings.Contains(err.Error(), "request 99999 doesn't exist") {
		t.Fatalf("err = %v", err)
	}
	if got := h.get(a).URL; got != "http://a" {
		t.Errorf("request %d's url is %q after a failed call", a, got)
	}
}

func TestEditManyNamesHeadersAsGetRequestDoes(t *testing.T) {
	h := newHarness(t)
	a := h.create(models.HttpRequest{Name: "a", Method: "GET"})
	err := h.runErr("request.edit_many", map[string]any{"changes": []map[string]any{
		{"id": a, "field": "headers", "value": "X: 1"},
	}})
	if err == nil || !strings.Contains(err.Error(), "request_headers") {
		t.Fatalf("err = %v, want a pointer to request_headers", err)
	}
}

func TestEditManyIsOneUndoStep(t *testing.T) {
	h := newHarness(t)
	a := h.create(models.HttpRequest{Name: "a", Method: "GET", URL: "http://a", Body: "old"})
	b := h.create(models.HttpRequest{Name: "b", Method: "GET", URL: "http://b"})

	h.run("request.edit_many", map[string]any{"changes": []map[string]any{
		{"id": a, "field": "body", "value": "first"},
		{"id": a, "field": "body", "value": "second"},
		{"id": b, "field": "url", "value": "http://new"},
	}}, nil)
	if _, err := h.runner.Undo(); err != nil {
		t.Fatal(err)
	}
	if got := h.get(a).Body; got != "old" {
		t.Errorf("a's body after undo = %q, want old", got)
	}
	if got := h.get(b).URL; got != "http://b" {
		t.Errorf("b's url after undo = %q", got)
	}
}

func TestBulkSetRejectsANonStringForATextField(t *testing.T) {
	h := newHarness(t)
	a := h.create(models.HttpRequest{Name: "a", Method: "GET"})
	err := h.runErr("request.bulk_set", map[string]any{"ids": []int64{a}, "field": "body", "value": map[string]any{"x": 1}})
	if err == nil || !strings.Contains(err.Error(), "body must be a string") || strings.Contains(err.Error(), "sql") {
		t.Fatalf("err = %v, want a plain message, not a database error", err)
	}
}

func TestCreateManyKeepsTheGivenOrderAndIsOneUndoStep(t *testing.T) {
	h := newHarness(t)
	var created []models.HttpRequest
	h.run("request.create_many", map[string]any{"requests": []map[string]any{
		{"collection_id": h.collectionID, "name": "first", "url": "http://x/1", "body": `{"a": 1}`, "request_headers": "A: 1"},
		{"collection_id": h.collectionID, "name": "second", "method": "post"},
		{"collection_id": h.collectionID, "name": "third"},
	}}, &created)

	if len(created) != 3 {
		t.Fatalf("created %d requests", len(created))
	}
	for i, want := range []string{"first", "second", "third"} {
		if created[i].Name != want {
			t.Errorf("created[%d] = %q, want %q", i, created[i].Name, want)
		}
		if i > 0 && created[i].UserSortOrder <= created[i-1].UserSortOrder {
			t.Errorf("%s isn't after %s in the collection", created[i].Name, created[i-1].Name)
		}
	}
	if created[0].Body != `{"a": 1}` || created[0].RequestHeaders != "A: 1" || created[0].Method != "GET" || created[1].Method != "POST" {
		t.Errorf("content not stored: %+v", created[:2])
	}

	if _, err := h.runner.Undo(); err != nil {
		t.Fatal(err)
	}
	for _, r := range created {
		if err := h.runErr("request.get", map[string]any{"id": r.ID}); err == nil {
			t.Errorf("request %s survived the undo", r.Name)
		}
	}
}

func TestCreateManyCreatesNothingWhenOneFails(t *testing.T) {
	h := newHarness(t)
	var before int
	h.db.QueryRow(`SELECT COUNT(*) FROM http_requests`).Scan(&before)

	err := h.runErr("request.create_many", map[string]any{"requests": []map[string]any{
		{"collection_id": h.collectionID, "name": "ok"},
		{"collection_id": h.collectionID, "name": "bad", "body": map[string]any{"priority": 5}},
	}})
	if err == nil || !strings.Contains(err.Error(), "request 2 of 2") || !strings.Contains(err.Error(), "body must be a string") {
		t.Fatalf("err = %v", err)
	}
	var after int
	h.db.QueryRow(`SELECT COUNT(*) FROM http_requests`).Scan(&after)
	if after != before {
		t.Errorf("%d requests were created by a failed call", after-before)
	}
}
