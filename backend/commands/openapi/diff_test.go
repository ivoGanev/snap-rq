package openapi

import "testing"

func TestMatchKeyIgnoresServerQueryAndPlaceholderStyle(t *testing.T) {
	same := []string{
		"https://api.example.com/v1/pets/{{id}}?x={{x}}",
		"{{base_url}}/v1/pets/{{id}}",
		"/v1/pets/{id}/",
	}
	want := matchKey("GET", same[0])
	if want != "GET /v1/pets/{id}" {
		t.Fatalf("key = %q", want)
	}
	for _, u := range same[1:] {
		if got := matchKey("get", u); got != want {
			t.Fatalf("matchKey(%q) = %q, want %q", u, got, want)
		}
	}
	if matchKey("POST", same[0]) == want {
		t.Fatal("method must be part of the key")
	}
}

func TestDiffCollectionStatuses(t *testing.T) {
	seeds := []requestSeed{
		{Name: "a", Method: "GET", URL: "https://h/a", BodyType: ""},
		{Name: "b", Method: "GET", URL: "https://h/b?page={{page}}", Headers: "X-Trace: {{X-Trace}}"},
		{Name: "c", Method: "POST", URL: "https://h/c", BodyType: "json", Body: `{"a": 1, "b": 2}`},
	}
	stored := []storedRequest{
		{ID: 1, Method: "GET", URL: "{{base}}/a", BodyType: "json"},
		{ID: 2, Method: "GET", URL: "{{base}}/b", Headers: "X-Other: 1"},
		{ID: 3, Method: "DELETE", URL: "{{base}}/gone", BodyType: "json"},
	}
	diffs, gone := diffCollection(seeds, stored)

	status := map[string]requestStatus{}
	for _, d := range diffs {
		status[d.Key] = d.Status
	}
	if status["GET /a"] != statusUnchanged || status["GET /b"] != statusChanged || status["POST /c"] != statusNew {
		t.Fatalf("statuses = %v", status)
	}
	if len(gone) != 1 || gone[0] != "DELETE /gone" {
		t.Fatalf("not in spec = %v", gone)
	}

	var b requestDiff
	for _, d := range diffs {
		if d.Key == "GET /b" {
			b = d
		}
	}
	if !hasField(b.Changes, "query") || !hasField(b.Changes, "headers") || len(b.Changes) != 2 {
		t.Fatalf("changes for GET /b = %+v, want query and headers", b.Changes)
	}
}

func TestBodyWhitespaceIsNotAChange(t *testing.T) {
	seed := requestSeed{Method: "POST", URL: "https://h/c", BodyType: "json", Body: "{\n  \"a\": 1\n}"}
	stored := storedRequest{Method: "POST", URL: "https://h/c", BodyType: "json", Body: `{"a":1}`}
	if changes := compareFields(seed, stored); len(changes) != 0 {
		t.Fatalf("changes = %+v, want none", changes)
	}
}

func TestApplyChangesAddsWhatTheSpecHasAndKeepsTheUsersOwn(t *testing.T) {
	seed := requestSeed{
		Method: "GET", URL: "https://h/b?page={{page}}",
		Headers: "X-Trace: {{X-Trace}}", BodyType: "json",
	}
	stored := storedRequest{
		ID: 7, Name: "my own name", Method: "GET", URL: "{{base}}/b?old=1",
		Headers: "X-Other: keep-me\nAuthorization: Bearer secret", BodyType: "json", Body: "{}",
	}
	changes := compareFields(seed, stored)
	got := applyChanges(stored, seed, changes)

	if got.Name != "my own name" || got.ID != 7 {
		t.Fatalf("name or id changed: %+v", got)
	}
	if got.URL != "{{base}}/b?old=1&page={{page}}" {
		t.Fatalf("url = %q, want the existing query kept and the spec's added", got.URL)
	}
	if got.Headers != "X-Other: keep-me\nAuthorization: Bearer secret\nX-Trace: {{X-Trace}}" {
		t.Fatalf("headers = %q, want the user's headers kept and the spec's added", got.Headers)
	}
	if got.Body != "{}" {
		t.Fatalf("body was rewritten: %q", got.Body)
	}
}
