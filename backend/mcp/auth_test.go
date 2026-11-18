package mcp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGuard(t *testing.T) {
	handler := Guard(func() string { return "secret" }, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	cases := []struct {
		name   string
		host   string
		origin string
		auth   string
		want   int
	}{
		{"ok", "127.0.0.1:47821", "", "Bearer secret", http.StatusNoContent},
		{"ok localhost", "localhost:47821", "", "Bearer secret", http.StatusNoContent},
		{"ok ipv6", "[::1]:47821", "", "Bearer secret", http.StatusNoContent},
		{"loopback origin", "127.0.0.1:47821", "http://localhost:3000", "Bearer secret", http.StatusNoContent},
		{"no token", "127.0.0.1:47821", "", "", http.StatusUnauthorized},
		{"wrong token", "127.0.0.1:47821", "", "Bearer nope", http.StatusUnauthorized},
		{"token without scheme", "127.0.0.1:47821", "", "secret", http.StatusUnauthorized},
		{"rebinding host", "evil.example:47821", "", "Bearer secret", http.StatusForbidden},
		{"web page origin", "127.0.0.1:47821", "https://evil.example", "Bearer secret", http.StatusForbidden},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		req.Host = tc.host
		if tc.origin != "" {
			req.Header.Set("Origin", tc.origin)
		}
		if tc.auth != "" {
			req.Header.Set("Authorization", tc.auth)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("%s: status %d, want %d", tc.name, rec.Code, tc.want)
		}
	}
}

func TestGuardRefusesWhenNoTokenIsSet(t *testing.T) {
	handler := Guard(func() string { return "" }, http.NotFoundHandler())
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Host = "127.0.0.1:47821"
	req.Header.Set("Authorization", "Bearer ")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", rec.Code)
	}
}

func TestGenerateTokenIsRandomAndLong(t *testing.T) {
	a, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := GenerateToken()
	if a == b || len(a) != 64 {
		t.Fatalf("tokens %q %q", a, b)
	}
}
