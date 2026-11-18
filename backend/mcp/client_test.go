package mcp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Stateless requests have no handshake, so the caller is named from its
// User-Agent.
func TestClientNameFallsBackToUserAgent(t *testing.T) {
	var got string
	h := rememberUserAgent(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = clientName(r.Context(), nil)
	}))
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("User-Agent", "claude-code/2.0")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if got != "claude-code/2.0" {
		t.Fatalf("client = %q, want the User-Agent", got)
	}

	req = httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("User-Agent", "")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if got != "" {
		t.Fatalf("client = %q, want empty so the dialog says an AI client", got)
	}
}
