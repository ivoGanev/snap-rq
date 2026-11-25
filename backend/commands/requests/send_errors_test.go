package requests_test

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"snap-rq/backend/models"
)

// A failed send is saved as a status-0 response whose body the error view
// shows: the friendly text first, the raw error after "Details: ".
func sendFails(t *testing.T, h *harness, req models.HttpRequest) string {
	t.Helper()
	res := h.mustSend(h.create(req), sendOptions{})
	if res.StatusCode != 0 {
		t.Fatalf("status %d, want a failed send", res.StatusCode)
	}
	return res.Body
}

func expectFriendly(t *testing.T, body, friendly, raw string) {
	t.Helper()
	if !strings.HasPrefix(body, friendly) {
		t.Errorf("body = %q, want it to start with %q", body, friendly)
	}
	if !strings.Contains(body, "\n\nDetails: ") || !strings.Contains(body, raw) {
		t.Errorf("body = %q, want the raw error %q under Details", body, raw)
	}
}

func TestAHeaderLineThatIsntAHeaderStopsTheSend(t *testing.T) {
	for _, tc := range []struct{ name, headers, want string }{
		{"JSON typed as headers", "X-One: 1\n{\"Accept\": \"application/json\"}",
			"Header line 2 isn't a header: {\"Accept\": \"application/json\"}"},
		{"no colon", "X-One: 1\n\nnot a header", "Header line 3 isn't a header: not a header"},
		{"space in the name", "Content Type: text/plain", "Header line 1 isn't a header: Content Type: text/plain"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			body := sendFails(t, h, models.HttpRequest{Method: "GET", URL: h.url("/x"), RequestHeaders: tc.headers})
			if !strings.HasPrefix(body, tc.want) || !strings.Contains(body, "Name: value") {
				t.Errorf("body = %q, want %q and how headers are written", body, tc.want)
			}
			if n := len(h.requests()); n != 0 {
				t.Errorf("the server got %d requests, want none", n)
			}
		})
	}
}

func TestClientErrorsAreExplained(t *testing.T) {
	closed := func(w http.ResponseWriter, r *http.Request) {
		conn, _, _ := w.(http.Hijacker).Hijack()
		conn.Close()
	}
	hangup := httptest.NewServer(http.HandlerFunc(closed))
	defer hangup.Close()
	loop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.Path, http.StatusFound)
	}))
	defer loop.Close()
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer tlsServer.Close()
	// A port nothing listens on: taken, then released.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	refused := listener.Addr().String()
	listener.Close()

	for _, tc := range []struct{ name, url, friendly, raw string }{
		{"unknown host", "http://does-not-exist.invalid/x",
			"Couldn't find the server does-not-exist.invalid.", "request failed:"},
		{"connection refused", "http://" + refused + "/x",
			fmt.Sprintf("Nothing is answering at %s.", refused), "request failed:"},
		{"connection closed", hangup.URL, "The server closed the connection before answering.", "request failed:"},
		{"redirect loop", loop.URL + "/again", "Stopped after 10 redirects", "request failed:"},
		{"untrusted certificate", tlsServer.URL,
			"The certificate of " + strings.TrimPrefix(tlsServer.URL, "https://") + " isn't signed by an authority",
			"request failed:"},
		{"unsupported scheme", "ftp://example.com/x", "The URL has to start with http:// or https://.", "request failed:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			expectFriendly(t, sendFails(t, h, models.HttpRequest{Method: "GET", URL: tc.url}), tc.friendly, tc.raw)
		})
	}
}

func TestATimeoutSaysHowLongItWaited(t *testing.T) {
	h := newHarness(t)
	if _, err := h.db.Exec(`INSERT OR REPLACE INTO app_settings (key, value) VALUES ('request_timeout_seconds', '1')`); err != nil {
		t.Fatal(err)
	}
	body := sendFails(t, h, models.HttpRequest{Method: "GET", URL: h.url("/slow?ms=3000")})
	expectFriendly(t, body, "The server didn't answer within 1s.", "request failed:")
}

func TestAMissingFormFileIsNamed(t *testing.T) {
	h := newHarness(t)
	body := sendFails(t, h, models.HttpRequest{
		Method: "POST", URL: h.url("/upload"), BodyType: "form-data",
		BodyForm: `[{"key":"avatar","type":"file","file_path":"/no/such/dir/me.png","file_name":"me.png","enabled":true}]`,
	})
	expectFriendly(t, body, "The file me.png for the form field avatar can't be found.", "opening file")
}

func TestAnErrorWeDontRecogniseIsShownAsItIs(t *testing.T) {
	h := newHarness(t)
	body := sendFails(t, h, models.HttpRequest{Method: "GET", URL: "http://[::1"})
	if strings.Contains(body, "Details: ") || !strings.HasPrefix(body, "building request:") {
		t.Errorf("body = %q, want the raw error alone", body)
	}
}
