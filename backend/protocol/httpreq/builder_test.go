package httpreq

import (
	"context"
	"io"
	"mime"
	"mime/multipart"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"snap-rq/backend/models"
)

var env = models.Environment{Variables: []models.EnvironmentVariable{
	{Key: "base_url", Value: "http://x"},
	{Key: "fleet-management", Value: "fm"},
	{Key: "api.v2", Value: "v2"},
	{Key: "NAME", Value: "Ada"},
	{Key: "TOKEN", Value: "s3cr3t", Secret: true},
	{Key: "LOCKED", Secret: true, Locked: true},
}}

func TestResolveFillsEveryAllowedNameWithPadding(t *testing.T) {
	req, locked := New(env).Resolve(models.HttpRequest{URL: "{{base_url}}/{{ fleet-management }}/{{api.v2}}/{{missing}}"})
	if want := "http://x/fm/v2/{{missing}}"; req.URL != want || len(locked) != 0 {
		t.Errorf("URL = %q (locked %v), want %q", req.URL, locked, want)
	}
}

func TestResolveFillsHeadersBodyAndFormTextButNotFilePaths(t *testing.T) {
	req, _ := New(env).Resolve(models.HttpRequest{
		RequestHeaders: "Authorization: Bearer {{TOKEN}}",
		Body:           `{"name":"{{NAME}}"}`,
		BodyForm: `[{"key":"t","type":"text","value":"Hi {{NAME}}","enabled":true},
			{"key":"f","type":"file","file_path":"/files/{{NAME}}.txt","enabled":true}]`,
	})
	if req.RequestHeaders != "Authorization: Bearer s3cr3t" || req.Body != `{"name":"Ada"}` {
		t.Errorf("headers %q, body %q", req.RequestHeaders, req.Body)
	}
	fields, err := models.ParseFormFields(req.BodyForm)
	if err != nil || fields[0].Value != "Hi Ada" || fields[1].FilePath != "/files/{{NAME}}.txt" {
		t.Errorf("form = %+v (%v), want text filled and the file path kept", fields, err)
	}
}

func TestResolveReportsLockedSecretsAndKeepsThem(t *testing.T) {
	req, locked := New(env).Resolve(models.HttpRequest{URL: "https://{{NAME}}/{{LOCKED}}", RequestHeaders: "X: {{LOCKED}}"})
	if req.URL != "https://Ada/{{LOCKED}}" || req.RequestHeaders != "X: {{LOCKED}}" {
		t.Errorf("URL %q, headers %q", req.URL, req.RequestHeaders)
	}
	if !reflect.DeepEqual(locked, []string{"LOCKED", "LOCKED"}) {
		t.Errorf("locked = %v", locked)
	}
}

func TestResolvingTwiceFillsPlaceholdersAScriptWrote(t *testing.T) {
	b := New(env)
	req, _ := b.Resolve(models.HttpRequest{URL: "{{base_url}}"})
	req.RequestHeaders = "X-Who: {{NAME}}" // as a beforeRequest script would
	req, _ = b.Resolve(req)
	if req.URL != "http://x" || req.RequestHeaders != "X-Who: Ada" {
		t.Errorf("URL %q, headers %q", req.URL, req.RequestHeaders)
	}
}

func TestBuildMethodAndScheme(t *testing.T) {
	for _, tc := range []struct{ method, url, wantMethod, wantURL string }{
		{"", "example.com/a", "GET", "http://example.com/a"},
		{" post ", "https://example.com", "POST", "https://example.com"},
		{"patch", "", "PATCH", ""},
	} {
		out, err := Build(models.HttpRequest{Method: tc.method, URL: tc.url})
		if err != nil || out.Method != tc.wantMethod || out.URL != tc.wantURL {
			t.Errorf("Build(%q, %q) = %q %q (%v)", tc.method, tc.url, out.Method, out.URL, err)
		}
	}
}

func TestBuildRejectsLinesThatArentHeaders(t *testing.T) {
	for _, tc := range []struct{ headers, want string }{
		{`{"Accept": "application/json"}`, "Header line 1 isn't a header"},
		{"X-One: 1\n\nnot a header", "Header line 3 isn't a header: not a header"},
		{"Content Type: text/plain", "Header line 1 isn't a header"},
	} {
		out, err := Build(models.HttpRequest{Method: "GET", URL: "example.com", RequestHeaders: tc.headers})
		if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "Name: value") {
			t.Errorf("headers %q: error %v, want %q", tc.headers, err, tc.want)
		}
		if out.Method != "GET" || out.URL != "http://example.com" {
			t.Errorf("a failed build lost its method and URL: %q %q", out.Method, out.URL)
		}
	}
}

func TestBuildContentType(t *testing.T) {
	for _, tc := range []struct{ bodyType, body, headers, want string }{
		{"json", `{"a":1}`, "", "application/json"},
		{"", `{"a":1}`, "", "application/json"},
		{"text", "hello", "", "text/plain"},
		{"json", `{"a":1}`, "content-type: application/vnd.api+json", "application/vnd.api+json"},
		{"json", "", "", ""},
	} {
		out, err := Build(models.HttpRequest{URL: "x", BodyType: tc.bodyType, Body: tc.body, RequestHeaders: tc.headers})
		if err != nil || out.Header.Get("Content-Type") != tc.want {
			t.Errorf("%s %q with %q: Content-Type %q (%v), want %q", tc.bodyType, tc.body, tc.headers,
				out.Header.Get("Content-Type"), err, tc.want)
		}
	}
}

func TestBuildFormDataKeepsEnabledFieldsAndIsFormEvenWhenEmpty(t *testing.T) {
	out, err := Build(models.HttpRequest{URL: "x", BodyType: "form-data", BodyForm: `[
		{"key":"a","type":"text","value":"1","enabled":true},
		{"key":"off","type":"text","value":"2","enabled":false},
		{"key":"","type":"text","value":"3","enabled":true}]`})
	if err != nil || !out.FormData || len(out.Form) != 1 || out.Form[0].Key != "a" {
		t.Errorf("form = %+v (%v)", out.Form, err)
	}
	empty, err := Build(models.HttpRequest{URL: "x", BodyType: "form-data", BodyForm: `[]`})
	if err != nil || !empty.FormData {
		t.Errorf("a form-data body with no fields isn't form-data (%v)", err)
	}
	if _, err := Build(models.HttpRequest{URL: "x", BodyType: "form-data", BodyForm: "{bad"}); err == nil {
		t.Error("an unreadable form body built")
	}
}

func TestHTTPRequestEncodesTheFormWithItsFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "photo.bin")
	if err := os.WriteFile(path, []byte{1, 2, 3}, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := Build(models.HttpRequest{Method: "POST", URL: "http://x", BodyType: "form-data", BodyForm: `[
		{"key":"title","type":"text","value":"Hi","enabled":true},
		{"key":"photo","type":"file","file_path":"` + filepath.ToSlash(path) + `","file_name":"me.png","enabled":true}]`})
	if err != nil {
		t.Fatal(err)
	}
	httpReq, err := out.HTTPRequest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, params, err := mime.ParseMediaType(httpReq.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("Content-Type %q", httpReq.Header.Get("Content-Type"))
	}
	form, err := multipart.NewReader(httpReq.Body, params["boundary"]).ReadForm(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	if form.Value["title"][0] != "Hi" || form.File["photo"][0].Filename != "me.png" {
		t.Errorf("form = %+v", form)
	}
	f, _ := form.File["photo"][0].Open()
	if data, _ := io.ReadAll(f); string(data) != "\x01\x02\x03" {
		t.Errorf("file bytes = %v", data)
	}
}

func TestHTTPRequestNamesAMissingFile(t *testing.T) {
	out, _ := Build(models.HttpRequest{URL: "http://x", BodyType: "form-data",
		BodyForm: `[{"key":"doc","type":"file","file_path":"/no/such/file.txt","file_name":"file.txt","enabled":true}]`})
	_, err := out.HTTPRequest(context.Background())
	if err == nil || !strings.HasPrefix(err.Error(), "The file file.txt for the form field doc can't be found.") ||
		!strings.Contains(err.Error(), "\n\nDetails: opening file") {
		t.Errorf("error = %v", err)
	}
}

func TestHTTPRequestKeepsTheUsersContentTypeForAForm(t *testing.T) {
	out, _ := Build(models.HttpRequest{URL: "http://x", BodyType: "form-data", RequestHeaders: "Content-Type: custom/type",
		BodyForm: `[{"key":"a","type":"text","value":"1","enabled":true}]`})
	httpReq, err := out.HTTPRequest(context.Background())
	if err != nil || httpReq.Header.Get("Content-Type") != "custom/type" {
		t.Errorf("Content-Type %q (%v)", httpReq.Header.Get("Content-Type"), err)
	}
}

func TestHTTPRequestDoesntChangeTheOutgoingHeaders(t *testing.T) {
	out, _ := Build(models.HttpRequest{URL: "http://x", BodyType: "form-data",
		BodyForm: `[{"key":"a","type":"text","value":"1","enabled":true}]`})
	if _, err := out.HTTPRequest(context.Background()); err != nil {
		t.Fatal(err)
	}
	if out.Header.Get("Content-Type") != "" {
		t.Errorf("the multipart Content-Type leaked into Outgoing: %q", out.Header.Get("Content-Type"))
	}
}
