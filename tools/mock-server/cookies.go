package main

import (
	"encoding/json"
	"net/http"
	"time"
)

// registerCookieHandlers adds endpoints for manually testing the app's cookie jar.
func registerCookieHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/mock/cookies/set", cookiesSetHandler)
	mux.HandleFunc("/mock/cookies/set-flags", cookiesSetFlagsHandler)
	mux.HandleFunc("/mock/cookies/set-domain", cookiesSetDomainHandler)
	mux.HandleFunc("/mock/cookies/set-path", cookiesSetPathHandler)
	mux.HandleFunc("/mock/cookies/echo", cookiesEchoHandler)
	mux.HandleFunc("/mock/cookies/path/echo", cookiesEchoHandler)
	mux.HandleFunc("/mock/cookies/redirect", cookiesRedirectHandler)
	mux.HandleFunc("/mock/cookies/expire", cookiesExpireHandler)
}

func cookiesJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// cookiesSetHandler sets one session cookie and one persistent cookie (1 hour).
func cookiesSetHandler(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "session_id", Value: "abc123", Path: "/"})
	http.SetCookie(w, &http.Cookie{Name: "theme", Value: "dark", Path: "/", Expires: time.Now().Add(time.Hour)})
	cookiesJSON(w, http.StatusOK, map[string]string{"set": "session_id (session), theme (1 hour)"})
}

// cookiesSetFlagsHandler sets cookies with Secure, HttpOnly and SameSite.
func cookiesSetFlagsHandler(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: "secure_token", Value: "s3cret", Path: "/",
		Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
		MaxAge: 3600,
	})
	cookiesJSON(w, http.StatusOK, map[string]string{"set": "secure_token (Secure, HttpOnly, SameSite=Strict)"})
}

// cookiesSetDomainHandler sets a cookie with a Domain attribute (also matches subdomains).
func cookiesSetDomainHandler(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "domain_cookie", Value: "shared", Path: "/", Domain: "localhost", MaxAge: 3600})
	cookiesJSON(w, http.StatusOK, map[string]string{"set": "domain_cookie (Domain=localhost)"})
}

// cookiesSetPathHandler sets a cookie scoped to /mock/cookies/path only.
func cookiesSetPathHandler(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "scoped", Value: "only-here", Path: "/mock/cookies/path", MaxAge: 3600})
	cookiesJSON(w, http.StatusOK, map[string]string{
		"set":  "scoped (Path=/mock/cookies/path)",
		"hint": "sent to /mock/cookies/path/echo but not to /mock/cookies/echo",
	})
}

// cookiesEchoHandler returns the cookies the request carried.
func cookiesEchoHandler(w http.ResponseWriter, r *http.Request) {
	received := map[string]string{}
	for _, c := range r.Cookies() {
		received[c.Name] = c.Value
	}
	cookiesJSON(w, http.StatusOK, map[string]any{
		"path":          r.URL.Path,
		"cookie_header": r.Header.Get("Cookie"),
		"cookies":       received,
	})
}

// cookiesRedirectHandler sets a cookie and redirects to the echo, so the cookie
// must be stored and sent on the redirect hop.
func cookiesRedirectHandler(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "redirect_cookie", Value: "hop", Path: "/", MaxAge: 3600})
	http.Redirect(w, r, "/mock/cookies/echo", http.StatusFound)
}

// cookiesExpireHandler deletes a cookie (?name=, default session_id) with Max-Age=0.
func cookiesExpireHandler(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		name = "session_id"
	}
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1})
	cookiesJSON(w, http.StatusOK, map[string]string{"expired": name})
}
