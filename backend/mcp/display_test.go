package mcp

import (
	"path/filepath"
	"strings"
	"testing"

	"snap-rq/backend/database"
)

func displayService(t *testing.T) (*DisplayService, *Store) {
	t.Helper()
	db, err := database.OpenPath(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store := NewStore(db)
	return NewDisplayService(store), store
}

const loginBody = `{
  "user": {"id": 42, "username": "demo-user", "email": "user@example.test", "password": "hunter2"},
  "access_token": "tok-access-1",
  "refresh_token": "tok-refresh-2",
  "api_key": "key-3",
  "template_token": "{{token}}",
  "expires_in": 3600
}`

const loginHeaders = "Content-Type: application/json\n" +
	"X-Request-Id: req-0001\n" +
	"X-Auth-Token: tok-header-4\n" +
	"Set-Cookie: session_id=sess-5; Path=/; HttpOnly\n" +
	"Authorization: Bearer {{token}}"

func TestMaskResponseHidesSecretsAndKeepsTheRest(t *testing.T) {
	s, _ := displayService(t)
	got := s.MaskResponse("", loginHeaders, loginBody)

	for _, secret := range []string{"hunter2", "tok-access-1", "tok-refresh-2", "key-3", "tok-header-4", "sess-5"} {
		if strings.Contains(got.Body+got.Headers, secret) {
			t.Errorf("%q is still visible", secret)
		}
	}
	for _, kept := range []string{`"id": 42`, `"username": "demo-user"`, `"email": "user@example.test"`, `"expires_in": 3600`,
		`"template_token": "{{token}}"`, `"password": "` + DisplayHidden + `"`} {
		if !strings.Contains(got.Body, kept) {
			t.Errorf("body lacks %s:\n%s", kept, got.Body)
		}
	}
	for _, kept := range []string{"Content-Type: application/json", "X-Request-Id: req-0001", "Authorization: Bearer {{token}}",
		"X-Auth-Token: " + DisplayHidden} {
		if !strings.Contains(got.Headers, kept) {
			t.Errorf("headers lack %q:\n%s", kept, got.Headers)
		}
	}
	if !got.HeadersMasked || !got.BodyMasked {
		t.Errorf("masked flags = %v, %v; both sections hid something", got.HeadersMasked, got.BodyMasked)
	}
}

func TestMaskResponseWithNothingSecretIsUntouched(t *testing.T) {
	s, _ := displayService(t)
	headers := "Content-Type: application/json\nX-Request-Id: req-0001"
	body := `{"id": 1, "name": "plain", "items": [1, 2, 3]}`
	got := s.MaskResponse("https://example.com/items?page=2", headers, body)
	if got.URL != "https://example.com/items?page=2" || got.URLMasked ||
		got.Headers != headers || got.Body != body || got.HeadersMasked || got.BodyMasked {
		t.Errorf("got %+v, want everything as sent and no masked flags (the eye must not show)", got)
	}
}

func TestMaskResponseHidesTheUsersOwnNames(t *testing.T) {
	s, store := displayService(t)
	body := `{"pin_code": "1234"}`
	if got := s.MaskResponse("", "", body); got.BodyMasked {
		t.Fatalf("pin_code hidden before it was named: %s", got.Body)
	}
	if err := store.SetMaskExtra([]string{"pin_code"}); err != nil {
		t.Fatal(err)
	}
	if got := s.MaskResponse("", "", body); !got.BodyMasked || strings.Contains(got.Body, "1234") {
		t.Errorf("a name added in the settings isn't hidden: %s", got.Body)
	}
}

func TestMaskResponseHidesSecretsInTheURL(t *testing.T) {
	s, _ := displayService(t)
	got := s.MaskResponse("https://demo-user:hunter2@example.com/items?page=2&access_token=tok-access-1", "", "")
	if strings.Contains(got.URL, "hunter2") || strings.Contains(got.URL, "tok-access-1") {
		t.Errorf("a secret is still visible: %s", got.URL)
	}
	if !strings.Contains(got.URL, "page=2") || !strings.Contains(got.URL, "example.com/items") {
		t.Errorf("the rest of the URL is lost: %s", got.URL)
	}
	if !got.URLMasked {
		t.Error("url_masked is false, so the URL's eye won't show")
	}
}
