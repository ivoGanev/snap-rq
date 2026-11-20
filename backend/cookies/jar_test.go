package cookies

import (
	"database/sql"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"snap-rq/backend/secrets"

	_ "modernc.org/sqlite"
)

func newTestJar(t *testing.T, projectID int64) (*Jar, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	db.SetMaxOpenConns(1) // one in-memory database for every statement
	t.Cleanup(func() { db.Close() })
	// Cookies belong to projects (foreign key), so create two.
	secrets.SetDefault(secrets.NewVault(secrets.MemoryKeyStore()))
	if _, err := db.Exec(`CREATE TABLE projects (id INTEGER PRIMARY KEY); INSERT INTO projects (id) VALUES (1), (2);
		CREATE TABLE app_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
		CREATE TABLE environment_variables (id INTEGER PRIMARY KEY, environment_id INTEGER, key TEXT, value TEXT, secret INTEGER)`); err != nil {
		t.Fatalf("creating projects: %v", err)
	}
	if _, err := db.Exec(Schema); err != nil {
		t.Fatalf("creating schema: %v", err)
	}
	return New(db, projectID), db
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parsing %q: %v", raw, err)
	}
	return u
}

// names lists the cookie names the jar returns for a URL.
func names(j *Jar, raw string) string {
	u, _ := url.Parse(raw)
	var out []string
	for _, c := range j.Cookies(u) {
		out = append(out, c.Name+"="+c.Value)
	}
	return strings.Join(out, ",")
}

func TestHostOnlyAndDomainCookies(t *testing.T) {
	j, _ := newTestJar(t, 1)
	j.SetCookies(mustURL(t, "https://api.example.com/login"), []*http.Cookie{
		{Name: "host", Value: "1"},
		{Name: "wide", Value: "2", Domain: "example.com"},
	})

	if got := names(j, "https://api.example.com/x"); got != "host=1,wide=2" {
		t.Errorf("api.example.com got %q", got)
	}
	if got := names(j, "https://www.example.com/x"); got != "wide=2" {
		t.Errorf("subdomain got %q, want only the domain cookie", got)
	}
	if got := names(j, "https://example.com/x"); got != "wide=2" {
		t.Errorf("apex got %q, want only the domain cookie", got)
	}
	if got := names(j, "https://other.com/x"); got != "" {
		t.Errorf("other site got %q, want nothing", got)
	}
	if got := names(j, "https://notexample.com/x"); got != "" {
		t.Errorf("suffix lookalike got %q, want nothing", got)
	}
}

func TestRejectsForeignAndTooBroadDomains(t *testing.T) {
	j, _ := newTestJar(t, 1)
	u := mustURL(t, "https://api.example.com/")
	j.SetCookies(u, []*http.Cookie{
		{Name: "a", Value: "1", Domain: "other.com"},
		{Name: "b", Value: "1", Domain: "com"},
	})
	j.SetCookies(mustURL(t, "http://127.0.0.1/"), []*http.Cookie{{Name: "c", Value: "1", Domain: "0.0.1"}})

	for _, raw := range []string{"https://api.example.com/", "https://other.com/", "https://example.com/", "http://127.0.0.1/"} {
		if got := names(j, raw); got != "" {
			t.Errorf("%s got %q, want nothing", raw, got)
		}
	}
}

func TestPathMatchingAndOrder(t *testing.T) {
	j, _ := newTestJar(t, 1)
	u := mustURL(t, "https://example.com/api/login")
	j.SetCookies(u, []*http.Cookie{
		{Name: "root", Value: "1", Path: "/"},
		{Name: "api", Value: "2", Path: "/api"},
		{Name: "admin", Value: "3", Path: "/admin"},
	})

	if got := names(j, "https://example.com/api/users"); got != "api=2,root=1" {
		t.Errorf("/api/users got %q, want most specific path first", got)
	}
	if got := names(j, "https://example.com/apix"); got != "root=1" {
		t.Errorf("/apix got %q, want only root (path must match on a boundary)", got)
	}
	if got := names(j, "https://example.com"); got != "root=1" {
		t.Errorf("bare host got %q", got)
	}
}

func TestDefaultPathIsRequestDirectory(t *testing.T) {
	cases := map[string]string{
		"":               "/",
		"/":              "/",
		"/login":         "/",
		"/api/login":     "/api",
		"/a/b/c":         "/a/b",
		"noleadingslash": "/",
	}
	for in, want := range cases {
		if got := defaultPath(in); got != want {
			t.Errorf("defaultPath(%q) = %q, want %q", in, got, want)
		}
	}

	j, _ := newTestJar(t, 1)
	j.SetCookies(mustURL(t, "https://example.com/api/login"), []*http.Cookie{{Name: "s", Value: "1"}})
	if got := names(j, "https://example.com/api/users"); got != "s=1" {
		t.Errorf("inside default path got %q", got)
	}
	if got := names(j, "https://example.com/other"); got != "" {
		t.Errorf("outside default path got %q, want nothing", got)
	}
}

func TestSecureCookiesNeedHTTPSExceptLoopback(t *testing.T) {
	j, _ := newTestJar(t, 1)
	j.SetCookies(mustURL(t, "https://example.com/"), []*http.Cookie{{Name: "s", Value: "1", Secure: true}})
	j.SetCookies(mustURL(t, "http://localhost:8080/"), []*http.Cookie{{Name: "l", Value: "1", Secure: true}})

	if got := names(j, "https://example.com/"); got != "s=1" {
		t.Errorf("https got %q", got)
	}
	if got := names(j, "http://example.com/"); got != "" {
		t.Errorf("http got %q, want nothing", got)
	}
	if got := names(j, "http://localhost:8080/"); got != "l=1" {
		t.Errorf("loopback over http got %q, want the secure cookie", got)
	}
}

func TestExpiry(t *testing.T) {
	j, _ := newTestJar(t, 1)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	j.now = func() time.Time { return now }
	u := mustURL(t, "https://example.com/")

	j.SetCookies(u, []*http.Cookie{
		{Name: "session", Value: "1"},
		{Name: "maxage", Value: "1", MaxAge: 60},
		{Name: "dated", Value: "1", Expires: now.Add(time.Hour)},
	})
	if got := names(j, "https://example.com/"); got != "session=1,maxage=1,dated=1" {
		t.Fatalf("fresh got %q", got)
	}

	j.now = func() time.Time { return now.Add(2 * time.Minute) }
	if got := names(j, "https://example.com/"); got != "session=1,dated=1" {
		t.Errorf("after 2m got %q, want maxage gone", got)
	}

	j.now = func() time.Time { return now.Add(2 * time.Hour) }
	if got := names(j, "https://example.com/"); got != "session=1" {
		t.Errorf("after 2h got %q, want only the session cookie", got)
	}
}

func TestExpiredSetCookieDeletes(t *testing.T) {
	j, _ := newTestJar(t, 1)
	u := mustURL(t, "https://example.com/")
	j.SetCookies(u, []*http.Cookie{{Name: "a", Value: "1"}, {Name: "b", Value: "1"}, {Name: "c", Value: "1"}})

	j.SetCookies(u, []*http.Cookie{
		{Name: "a", MaxAge: -1},
		{Name: "b", Expires: time.Now().Add(-time.Hour)},
	})
	if got := names(j, "https://example.com/"); got != "c=1" {
		t.Errorf("got %q, want only c", got)
	}
}

func TestSetCookieReplacesSameIdentity(t *testing.T) {
	j, db := newTestJar(t, 1)
	u := mustURL(t, "https://example.com/")
	j.SetCookies(u, []*http.Cookie{{Name: "s", Value: "old"}})
	j.SetCookies(u, []*http.Cookie{{Name: "s", Value: "new"}})

	if got := names(j, "https://example.com/"); got != "s=new" {
		t.Errorf("got %q, want s=new", got)
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM cookies`).Scan(&n)
	if n != 1 {
		t.Errorf("%d rows, want 1", n)
	}
}

func TestJarsAreIsolatedPerProject(t *testing.T) {
	a, db := newTestJar(t, 1)
	b := New(db, 2)
	u := mustURL(t, "https://example.com/")
	a.SetCookies(u, []*http.Cookie{{Name: "s", Value: "a"}})

	if got := names(b, "https://example.com/"); got != "" {
		t.Errorf("project 2 saw %q", got)
	}
	if got := names(a, "https://example.com/"); got != "s=a" {
		t.Errorf("project 1 got %q", got)
	}
}

func TestListHidesExpiredAndOrders(t *testing.T) {
	j, db := newTestJar(t, 1)
	u := mustURL(t, "https://example.com/")
	j.SetCookies(u, []*http.Cookie{{Name: "b", Value: "1"}, {Name: "a", Value: "1"}})
	_, _ = db.Exec(`INSERT INTO cookies (project_id, domain, path, name, value, expires_at) VALUES (1, 'example.com', '/', 'old', 'x', 1)`)

	list, err := List(db, 1)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var got []string
	for _, c := range list {
		got = append(got, c.Name)
	}
	if strings.Join(got, ",") != "a,b" {
		t.Errorf("List = %v, want [a b]", got)
	}
}

func TestCookieValuesAreStoredEncrypted(t *testing.T) {
	j, db := newTestJar(t, 1)
	j.SetCookies(mustURL(t, "https://example.com/"), []*http.Cookie{{Name: "session", Value: "abc123"}})

	var stored string
	db.QueryRow(`SELECT value FROM cookies WHERE name = 'session'`).Scan(&stored)
	if !secrets.IsEncrypted(stored) || strings.Contains(stored, "abc123") {
		t.Errorf("stored value %q, want encrypted", stored)
	}
	if got := names(j, "https://example.com/"); got != "session=abc123" {
		t.Errorf("jar returned %q, want the decrypted value", got)
	}
}

func TestUnreadableCookieValuesAreDropped(t *testing.T) {
	j, db := newTestJar(t, 1)
	j.SetCookies(mustURL(t, "https://example.com/"), []*http.Cookie{{Name: "good", Value: "1"}})
	db.Exec(`INSERT INTO cookies (project_id, domain, path, name, value) VALUES (1, 'example.com', '/', 'plain', 'not-encrypted')`)

	if got := names(j, "https://example.com/"); got != "good=1" {
		t.Errorf("jar returned %q, want only the readable cookie", got)
	}
	var left int
	db.QueryRow(`SELECT COUNT(*) FROM cookies WHERE name = 'plain'`).Scan(&left)
	if left != 0 {
		t.Error("the unreadable cookie was not deleted")
	}
}

func TestLostKeyClearsCookiesWhenNoSecretVariablesNeedIt(t *testing.T) {
	j, db := newTestJar(t, 1)
	j.SetCookies(mustURL(t, "https://example.com/"), []*http.Cookie{{Name: "old", Value: "1"}})

	secrets.SetDefault(secrets.NewVault(secrets.MemoryKeyStore())) // the keyring entry is gone
	if got := names(j, "https://example.com/"); got != "" {
		t.Errorf("jar returned %q after losing the key, want nothing", got)
	}
	j.SetCookies(mustURL(t, "https://example.com/"), []*http.Cookie{{Name: "new", Value: "2"}})
	if got := names(j, "https://example.com/"); got != "new=2" {
		t.Errorf("jar returned %q, want the new cookie stored with a fresh key", got)
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM cookies`).Scan(&n)
	if n != 1 {
		t.Errorf("%d cookies stored, want only the new one", n)
	}
}

func TestLostKeyKeepsCookiesWhileSecretVariablesAreLocked(t *testing.T) {
	j, db := newTestJar(t, 1)
	j.SetCookies(mustURL(t, "https://example.com/"), []*http.Cookie{{Name: "kept", Value: "1"}})
	db.Exec(`INSERT INTO environment_variables (environment_id, key, value, secret) VALUES (1, 'API_KEY', 'v1:x', 1)`)

	secrets.SetDefault(secrets.NewVault(secrets.MemoryKeyStore()))
	if got := names(j, "https://example.com/"); got != "" {
		t.Errorf("jar sent %q while locked, want nothing", got)
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM cookies`).Scan(&n)
	if n != 1 {
		t.Errorf("%d cookies left, want the locked one kept until the user unlocks", n)
	}
}
