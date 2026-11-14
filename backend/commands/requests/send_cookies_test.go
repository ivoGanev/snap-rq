package requests

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"snap-rq/backend/cookies"
	"snap-rq/backend/models"

	_ "modernc.org/sqlite"
)

func newCookieTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	db.SetMaxOpenConns(1) // one in-memory database for every statement
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`
		CREATE TABLE projects (id INTEGER PRIMARY KEY);
		INSERT INTO projects (id) VALUES (1);
		CREATE TABLE app_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
	` + cookies.Schema); err != nil {
		t.Fatalf("creating schema: %v", err)
	}
	return db
}

// loginServer sets a session cookie on /login and echoes the Cookie header
// it receives on /whoami.
func loginServer() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "abc123", Path: "/"})
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/whoami", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.Header.Get("Cookie")))
	})
	mux.HandleFunc("/bounce", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "hop", Value: "1", Path: "/"})
		http.Redirect(w, r, "/whoami", http.StatusFound)
	})
	return httptest.NewServer(mux)
}

func TestSendStoresAndSendsCookies(t *testing.T) {
	db := newCookieTestDB(t)
	srv := loginServer()
	defer srv.Close()
	send := Send{db: db}

	get := func(path string) models.HttpResponse {
		return send.doHTTP(models.HttpRequest{ProjectID: 1, Method: "GET", URL: srv.URL + path}, nil)
	}

	if got := get("/whoami").Body; got != "" {
		t.Fatalf("before login the server saw cookies %q", got)
	}
	get("/login")
	if got := get("/whoami").Body; got != "session=abc123" {
		t.Errorf("after login the server saw %q, want session=abc123", got)
	}
}

func TestSendKeepsCookiesSetDuringRedirects(t *testing.T) {
	db := newCookieTestDB(t)
	srv := loginServer()
	defer srv.Close()
	send := Send{db: db}

	resp := send.doHTTP(models.HttpRequest{ProjectID: 1, Method: "GET", URL: srv.URL + "/bounce"}, nil)
	if resp.Body != "hop=1" {
		t.Errorf("redirect target saw %q, want the cookie set on the first hop", resp.Body)
	}
}

func TestSendMergesWithExplicitCookieHeader(t *testing.T) {
	db := newCookieTestDB(t)
	srv := loginServer()
	defer srv.Close()
	send := Send{db: db}

	send.doHTTP(models.HttpRequest{ProjectID: 1, Method: "GET", URL: srv.URL + "/login"}, nil)
	resp := send.doHTTP(models.HttpRequest{
		ProjectID: 1, Method: "GET", URL: srv.URL + "/whoami", RequestHeaders: "Cookie: manual=1",
	}, nil)
	if !strings.Contains(resp.Body, "manual=1") || !strings.Contains(resp.Body, "session=abc123") {
		t.Errorf("server saw %q, want both the typed and the stored cookie", resp.Body)
	}
}

func TestSendHonoursTheCookieJarSetting(t *testing.T) {
	db := newCookieTestDB(t)
	srv := loginServer()
	defer srv.Close()
	send := Send{db: db}
	if _, err := db.Exec(`INSERT INTO app_settings (key, value) VALUES ('use_cookie_jar', 'false')`); err != nil {
		t.Fatal(err)
	}

	send.doHTTP(models.HttpRequest{ProjectID: 1, Method: "GET", URL: srv.URL + "/login"}, nil)
	if got := send.doHTTP(models.HttpRequest{ProjectID: 1, Method: "GET", URL: srv.URL + "/whoami"}, nil).Body; got != "" {
		t.Errorf("with the jar off the server saw %q", got)
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM cookies`).Scan(&n)
	if n != 0 {
		t.Errorf("%d cookies stored with the jar off", n)
	}
}
