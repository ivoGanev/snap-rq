// Package cookies implements the per-project cookie jar: a database-backed
// http.CookieJar that stores the cookies servers set and returns the ones
// that match each outgoing request.
//
// The standard library jar cannot list or edit what it holds, so the app
// keeps its own. The matching rules follow RFC 6265 (domain, path, expiry,
// Secure), with one development-friendly exception: Secure cookies are also
// sent to loopback hosts over plain HTTP.
package cookies

import (
	"database/sql"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"snap-rq/backend/models"
)

// Schema creates the cookies table. Cookies are unique by project, domain,
// path and name, which is how RFC 6265 defines a cookie's identity.
const Schema = `
	CREATE TABLE IF NOT EXISTS cookies (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		project_id INTEGER NOT NULL,
		domain TEXT NOT NULL,
		host_only INTEGER NOT NULL DEFAULT 1,
		path TEXT NOT NULL DEFAULT '/',
		name TEXT NOT NULL,
		value TEXT NOT NULL DEFAULT '',
		expires_at INTEGER NOT NULL DEFAULT 0,
		secure INTEGER NOT NULL DEFAULT 0,
		http_only INTEGER NOT NULL DEFAULT 0,
		same_site TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL DEFAULT 0,
		UNIQUE (project_id, domain, path, name),
		FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
	);
	CREATE INDEX IF NOT EXISTS idx_cookies_project_id ON cookies(project_id);
`

// Jar is the cookie jar of one project.
type Jar struct {
	db        *sql.DB
	projectID int64
	now       func() time.Time
}

// New returns the jar for a project, reading and writing through db.
func New(db *sql.DB, projectID int64) *Jar {
	return &Jar{db: db, projectID: projectID, now: time.Now}
}

// SetCookies stores the cookies a response set for u. Cookies that arrive
// already expired (Max-Age<=0 or a past Expires) delete the stored cookie.
func (j *Jar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	if len(cookies) == 0 || u == nil {
		return
	}
	host := canonicalHost(u.Hostname())
	if host == "" {
		return
	}
	now := j.now()

	tx, err := j.db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()
	if EnsureUsable(tx) != nil {
		return // locked by secret variables: keep what's stored, store nothing new
	}

	for _, c := range cookies {
		if c == nil || c.Name == "" {
			continue
		}
		domain, hostOnly, ok := cookieDomain(host, c.Domain)
		if !ok {
			continue
		}
		path := c.Path
		if path == "" || path[0] != '/' {
			path = defaultPath(u.Path)
		}

		var expires int64
		remove := false
		switch {
		case c.MaxAge < 0:
			remove = true
		case c.MaxAge > 0:
			expires = now.Add(time.Duration(c.MaxAge) * time.Second).Unix()
		case !c.Expires.IsZero():
			if !c.Expires.After(now) {
				remove = true
			} else {
				expires = c.Expires.Unix()
			}
		}

		if remove {
			_, _ = tx.Exec(
				`DELETE FROM cookies WHERE project_id = ? AND domain = ? AND path = ? AND name = ?`,
				j.projectID, domain, path, c.Name)
			continue
		}
		sealed, err := Seal(tx, c.Value)
		if err != nil {
			continue
		}
		_, _ = tx.Exec(`
			INSERT INTO cookies
				(project_id, domain, host_only, path, name, value, expires_at, secure, http_only, same_site, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(project_id, domain, path, name) DO UPDATE SET
				value = excluded.value, host_only = excluded.host_only,
				expires_at = excluded.expires_at, secure = excluded.secure,
				http_only = excluded.http_only, same_site = excluded.same_site`,
			j.projectID, domain, hostOnly, path, c.Name, sealed, expires,
			c.Secure, c.HttpOnly, sameSiteName(c.SameSite), now.Unix())
	}
	// Housekeeping: drop cookies whose time has passed.
	_, _ = tx.Exec(
		`DELETE FROM cookies WHERE project_id = ? AND expires_at > 0 AND expires_at <= ?`,
		j.projectID, now.Unix())
	_ = tx.Commit()
}

// Cookies returns the cookies to send with a request to u, most specific
// path first (then oldest first), as RFC 6265 asks.
func (j *Jar) Cookies(u *url.URL) []*http.Cookie {
	if u == nil {
		return nil
	}
	host := canonicalHost(u.Hostname())
	if host == "" {
		return nil
	}
	if EnsureUsable(j.db) != nil {
		return nil // locked by secret variables: send none until unlocked
	}
	rows, err := j.db.Query(`
		SELECT id, domain, host_only, path, name, value, secure, created_at
		FROM cookies
		WHERE project_id = ? AND (expires_at = 0 OR expires_at > ?)`,
		j.projectID, j.now().Unix())
	if err != nil {
		return nil
	}
	defer rows.Close()

	type match struct {
		id      int64
		cookie  *http.Cookie // Value still encrypted until the rows are closed
		pathLen int
		created int64
	}
	var matches []match
	https := u.Scheme == "https"
	reqPath := u.Path
	if reqPath == "" {
		reqPath = "/"
	}
	for rows.Next() {
		var id int64
		var domain, path, name, value string
		var hostOnly, secure bool
		var created int64
		if err := rows.Scan(&id, &domain, &hostOnly, &path, &name, &value, &secure, &created); err != nil {
			continue
		}
		if !domainApplies(host, domain, hostOnly) || !pathMatches(reqPath, path) {
			continue
		}
		if secure && !https && !isLoopback(host) {
			continue
		}
		matches = append(matches, match{id, &http.Cookie{Name: name, Value: value}, len(path), created})
	}
	rows.Close()

	// Decrypt only the cookies that match (a few per request), once the
	// rows are closed; drop any that will never decrypt.
	usable := matches[:0]
	for _, m := range matches {
		plain, err := Open(j.db, m.cookie.Value)
		if err != nil {
			if !isLocked(err) {
				_, _ = j.db.Exec(`DELETE FROM cookies WHERE id = ?`, m.id)
			}
			continue
		}
		m.cookie.Value = plain
		usable = append(usable, m)
	}
	matches = usable
	sort.SliceStable(matches, func(a, b int) bool {
		if matches[a].pathLen != matches[b].pathLen {
			return matches[a].pathLen > matches[b].pathLen
		}
		return matches[a].created < matches[b].created
	})
	out := make([]*http.Cookie, len(matches))
	for i, m := range matches {
		out[i] = m.cookie
	}
	return out
}

// List returns every unexpired cookie of the project, ordered for display,
// with values decrypted. Values that can't be decrypted are left out.
func List(db Querier, projectID int64) ([]models.Cookie, error) {
	rows, err := db.Query(`
		SELECT id, project_id, domain, host_only, path, name, value, expires_at, secure, http_only, same_site, created_at
		FROM cookies WHERE project_id = ? AND (expires_at = 0 OR expires_at > ?)
		ORDER BY domain, path, name`, projectID, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []models.Cookie
	for rows.Next() {
		var c models.Cookie
		if err := rows.Scan(&c.ID, &c.ProjectID, &c.Domain, &c.HostOnly, &c.Path, &c.Name, &c.Value,
			&c.ExpiresAt, &c.Secure, &c.HTTPOnly, &c.SameSite, &c.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	readable := list[:0]
	for _, c := range list {
		plain, err := Open(db, c.Value)
		if err != nil {
			continue
		}
		c.Value = plain
		readable = append(readable, c)
	}
	return readable, nil
}

// canonicalHost lower-cases a host name and strips a trailing dot.
func canonicalHost(host string) string {
	return strings.TrimSuffix(strings.ToLower(host), ".")
}

func isIP(host string) bool { return net.ParseIP(host) != nil }

func isLoopback(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// cookieDomain resolves a Set-Cookie Domain attribute against the host that
// sent it. Without the attribute the cookie is host-only. A Domain the host
// does not belong to is rejected, as are Domain values for IP addresses.
func cookieDomain(host, attr string) (domain string, hostOnly, ok bool) {
	attr = canonicalHost(strings.TrimPrefix(attr, "."))
	if attr == "" || attr == host {
		return host, true, true
	}
	if isIP(host) || !strings.Contains(attr, ".") {
		return "", false, false
	}
	if !strings.HasSuffix(host, "."+attr) {
		return "", false, false
	}
	return attr, false, true
}

// domainApplies reports whether a stored cookie goes to host.
func domainApplies(host, domain string, hostOnly bool) bool {
	if host == domain {
		return true
	}
	if hostOnly || isIP(host) {
		return false
	}
	return strings.HasSuffix(host, "."+domain)
}

// pathMatches implements the RFC 6265 path-match rule.
func pathMatches(reqPath, cookiePath string) bool {
	if reqPath == cookiePath {
		return true
	}
	if !strings.HasPrefix(reqPath, cookiePath) {
		return false
	}
	return strings.HasSuffix(cookiePath, "/") || reqPath[len(cookiePath)] == '/'
}

// defaultPath is the directory of the request path, used when Set-Cookie
// names no Path.
func defaultPath(path string) string {
	if path == "" || path[0] != '/' {
		return "/"
	}
	i := strings.LastIndex(path, "/")
	if i == 0 {
		return "/"
	}
	return path[:i]
}

func sameSiteName(s http.SameSite) string {
	switch s {
	case http.SameSiteLaxMode:
		return "Lax"
	case http.SameSiteStrictMode:
		return "Strict"
	case http.SameSiteNoneMode:
		return "None"
	}
	return ""
}
