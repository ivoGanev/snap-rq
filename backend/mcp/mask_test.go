package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestHeadersMasking(t *testing.T) {
	m := NewMasker(nil)
	in := "Content-Type: application/json\nAuthorization: Bearer abc123\nX-Api-Key: k-999\nX-Trace: {{trace}}\nAuthorization: Bearer {{token}}"
	got := m.Headers(in)
	for _, leaked := range []string{"abc123", "k-999"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("leaked %q in %q", leaked, got)
		}
	}
	for _, kept := range []string{"Content-Type: application/json", "X-Trace: {{trace}}", "Authorization: Bearer {{token}}"} {
		if !strings.Contains(got, kept) {
			t.Fatalf("lost %q in %q", kept, got)
		}
	}
}

func TestURLMasking(t *testing.T) {
	m := NewMasker(nil)
	got := m.URL("https://user:hunter2@api.example.com/x?api_key=abc&page=2&token={{t}}")
	if strings.Contains(got, "hunter2") || strings.Contains(got, "abc") {
		t.Fatalf("leaked: %s", got)
	}
	if !strings.Contains(got, "page=2") || !strings.Contains(got, "token={{t}}") {
		t.Fatalf("over-masked: %s", got)
	}
}

func TestBodyMaskingKeepsLayout(t *testing.T) {
	m := NewMasker(nil)
	in := "{\n  \"user\": \"ann\",\n  \"password\": \"s3cret\",\n  \"nested\": {\"access_token\": \"zzz\", \"ok\": \"{{x}}\"},\n  \"client_secret\": \"{{secret}}\"\n}"
	got := m.Body(in)
	if strings.Contains(got, "s3cret") || strings.Contains(got, "zzz") {
		t.Fatalf("leaked: %s", got)
	}
	if !strings.Contains(got, "\"user\": \"ann\"") || !strings.Contains(got, "{{secret}}") {
		t.Fatalf("over-masked: %s", got)
	}
	if !json.Valid([]byte(got)) {
		t.Fatalf("result is not valid JSON: %s", got)
	}
	if form := m.Body("grant_type=password&client_secret=abc&scope=read"); strings.Contains(form, "abc") || !strings.Contains(form, "scope=read") {
		t.Fatalf("form body: %s", form)
	}
	if plain := m.Body("just some text with a password in it"); plain != "just some text with a password in it" {
		t.Fatalf("plain text changed: %s", plain)
	}
}

func TestBodyFormMasking(t *testing.T) {
	m := NewMasker(nil)
	got := m.BodyForm(`[{"key":"password","type":"text","value":"pw1","enabled":true},{"key":"name","type":"text","value":"bob","enabled":true}]`)
	if strings.Contains(got, "pw1") || !strings.Contains(got, "bob") {
		t.Fatalf("got %s", got)
	}
}

func TestExtraNames(t *testing.T) {
	if got := NewMasker(nil).Value("x-internal", "v"); got != "v" {
		t.Fatalf("masked without being asked: %s", got)
	}
	if got := NewMasker([]string{"X-Internal"}).Value("x-internal-id", "v"); got != Redacted {
		t.Fatalf("extra name ignored: %s", got)
	}
}

func TestPresentMasksAndCanBeSwitchedOff(t *testing.T) {
	s := NewServer(&fakeRunner{}, nil, "t")
	row := map[string]any{"id": 5, "url": "http://h/?token=abc", "request_headers": "Authorization: Bearer abc", "name": "token test"}
	out, err := s.present(row)
	if err != nil {
		t.Fatal(err)
	}
	text, _ := json.Marshal(out)
	if strings.Contains(string(text), "abc") || !strings.Contains(string(text), `"id":5`) || !strings.Contains(string(text), "token test") {
		t.Fatalf("got %s", text)
	}
	s.applyConfig(Config{MaskSecrets: false})
	out, _ = s.present(row)
	if text, _ = json.Marshal(out); !strings.Contains(string(text), "abc") {
		t.Fatalf("masking off must pass data through: %s", text)
	}
}

func TestInputWithRedactedIsRefused(t *testing.T) {
	s := NewServer(&fakeRunner{}, nil, "t")
	if err := s.checkInput(map[string]any{"value": "Authorization: Bearer " + Redacted}); err == nil {
		t.Fatal("a masked value must not be writable")
	}
	if err := s.checkInput(map[string]any{"value": "Authorization: Bearer {{token}}"}); err != nil {
		t.Fatal(err)
	}
	s.applyConfig(Config{MaskSecrets: false})
	if err := s.checkInput(map[string]any{"value": Redacted}); err != nil {
		t.Fatalf("with masking off nothing is refused: %v", err)
	}
}

// A move reads the real request and writes it back: the stored token must
// survive, even though a client would have been shown it masked.
func TestMoveRequestsWritesRealData(t *testing.T) {
	f := &fakeRunner{results: map[string]any{
		"request.get": map[string]any{"id": 7, "collection_id": 1, "request_headers": "Authorization: Bearer real-token", "url": "http://u?api_key=real-key"},
	}}
	if _, err := moveRequests(NewServer(f, nil, "t"), moveRequestsInput{IDs: []int64{7}, CollectionID: 2}); err != nil {
		t.Fatal(err)
	}
	update := f.calls[1].input
	if update["request_headers"] != "Authorization: Bearer real-token" || update["url"] != "http://u?api_key=real-key" {
		t.Fatalf("the update lost real data: %v", update)
	}
}
