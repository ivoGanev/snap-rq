package runnables

import (
	"strings"
	"testing"

	"snap-rq/backend/models"
)

func TestRequestToCurlGet(t *testing.T) {
	req := models.HttpRequest{
		URL:    "https://example.com/api/users",
		Method: "GET",
	}
	curl := RequestToCurl(req)
	if !strings.Contains(curl, "curl 'https://example.com/api/users'") {
		t.Errorf("expected curl GET command, got: %s", curl)
	}
	if strings.Contains(curl, "-X") {
		t.Errorf("did not expect explicit method for GET without body, got: %s", curl)
	}
}

func TestRequestToCurlPostWithHeadersAndBody(t *testing.T) {
	req := models.HttpRequest{
		URL:            "https://example.com/api/users",
		Method:         "POST",
		Body:           `{"name":"alice"}`,
		RequestHeaders: "Content-Type: application/json\nAuthorization: Bearer token123",
	}
	curl := RequestToCurl(req)

	if !strings.Contains(curl, "curl -X POST 'https://example.com/api/users'") {
		t.Errorf("expected curl POST command, got: %s", curl)
	}
	if !strings.Contains(curl, "-H 'Content-Type: application/json'") {
		t.Errorf("missing Content-Type header, got: %s", curl)
	}
	if !strings.Contains(curl, "-H 'Authorization: Bearer token123'") {
		t.Errorf("missing Authorization header, got: %s", curl)
	}
	if !strings.Contains(curl, "--data-raw '{\"name\":\"alice\"}'") {
		t.Errorf("missing body, got: %s", curl)
	}
}

func TestRequestToCurlEscapesSingleQuotes(t *testing.T) {
	req := models.HttpRequest{
		URL:    "https://example.com/api?q=it's",
		Method: "GET",
	}
	curl := RequestToCurl(req)
	if strings.Contains(curl, "'https://example.com/api?q=it's'") {
		t.Errorf("single quotes should be escaped, got: %s", curl)
	}
}

func TestCurlToRequestGet(t *testing.T) {
	curl := "curl 'https://example.com/api/users'"
	req, err := CurlToRequest(5, curl)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.CollectionID != 5 {
		t.Errorf("expected collection_id 5, got %d", req.CollectionID)
	}
	if req.URL != "https://example.com/api/users" {
		t.Errorf("expected URL, got %s", req.URL)
	}
	if req.Method != "GET" {
		t.Errorf("expected GET, got %s", req.Method)
	}
	if req.Body != "" {
		t.Errorf("expected empty body, got %s", req.Body)
	}
}

func TestCurlToRequestPostWithFlags(t *testing.T) {
	curl := `curl -X POST -H "Content-Type: application/json" -d '{"name":"alice"}' https://example.com/api/users`
	req, err := CurlToRequest(1, curl)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Method != "POST" {
		t.Errorf("expected POST, got %s", req.Method)
	}
	if req.URL != "https://example.com/api/users" {
		t.Errorf("expected URL, got %s", req.URL)
	}
	if req.Body != `{"name":"alice"}` {
		t.Errorf("expected body, got %s", req.Body)
	}
	if req.RequestHeaders != "Content-Type: application/json" {
		t.Errorf("expected header, got %s", req.RequestHeaders)
	}
}

func TestCurlToRequestDefaultsBodyToPost(t *testing.T) {
	curl := `curl -d 'payload' https://example.com/api`
	req, err := CurlToRequest(1, curl)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Method != "POST" {
		t.Errorf("expected POST when body is present, got %s", req.Method)
	}
}

func TestCurlToRequestMissingURL(t *testing.T) {
	curl := "curl -X POST -d 'body'"
	_, err := CurlToRequest(1, curl)
	if err == nil {
		t.Fatal("expected error for missing URL")
	}
}

func TestCurlToRequestIgnoresUnknownFlags(t *testing.T) {
	curl := "curl -s -X DELETE localhost:8099/steer/tenant-1"
	req, err := CurlToRequest(1, curl)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Method != "DELETE" {
		t.Errorf("expected DELETE, got %s", req.Method)
	}
	if req.URL != "localhost:8099/steer/tenant-1" {
		t.Errorf("expected URL, got %s", req.URL)
	}
}

func TestCurlToRequestIgnoresBundledAndValueFlags(t *testing.T) {
	curl := "curl -sSL -X POST https://example.com/api"
	req, err := CurlToRequest(1, curl)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Method != "POST" {
		t.Errorf("expected POST, got %s", req.Method)
	}
	if req.URL != "https://example.com/api" {
		t.Errorf("expected URL, got %s", req.URL)
	}
}

func TestCurlToRequestMultilineWithUrlAndCookieFlags(t *testing.T) {
	curl := "curl --request POST \\\n" +
		"  --url http://localhost:9090/v1/jobs \\\n" +
		"  --header 'Content-Type: application/json' \\\n" +
		"  --cookie JSESSIONID.ab14fcc9=node01sk8ejmoddvx0y97974iu7rrc3.node0 \\\n" +
		"  --data '{\n\t\"priority\": 5\n}'"
	req, err := CurlToRequest(1, curl)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Method != "POST" {
		t.Errorf("expected POST, got %s", req.Method)
	}
	if req.URL != "http://localhost:9090/v1/jobs" {
		t.Errorf("expected URL, got %s", req.URL)
	}
	if !strings.Contains(req.RequestHeaders, "Content-Type: application/json") {
		t.Errorf("missing Content-Type header, got %s", req.RequestHeaders)
	}
	if !strings.Contains(req.RequestHeaders, "Cookie: JSESSIONID.ab14fcc9=node01sk8ejmoddvx0y97974iu7rrc3.node0") {
		t.Errorf("missing Cookie header, got %s", req.RequestHeaders)
	}
	if !strings.Contains(req.Body, `"priority": 5`) {
		t.Errorf("unexpected body, got %s", req.Body)
	}
}

func TestRoundTripCurl(t *testing.T) {
	original := models.HttpRequest{
		URL:            "https://example.com/api/users",
		Method:         "PUT",
		Body:           `{"active":true}`,
		RequestHeaders: "Content-Type: application/json",
	}
	curl := RequestToCurl(original)
	parsed, err := CurlToRequest(original.CollectionID, curl)
	if err != nil {
		t.Fatalf("round trip failed: %v", err)
	}
	if parsed.URL != original.URL {
		t.Errorf("URL mismatch: %s vs %s", parsed.URL, original.URL)
	}
	if parsed.Method != original.Method {
		t.Errorf("method mismatch: %s vs %s", parsed.Method, original.Method)
	}
	if parsed.Body != original.Body {
		t.Errorf("body mismatch: %s vs %s", parsed.Body, original.Body)
	}
}
