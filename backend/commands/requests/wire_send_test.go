package requests_test

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"snap-rq/backend/models"
)

func TestTheRequestGoesOutAsTyped(t *testing.T) {
	h := newHarness(t)
	id := h.create(models.HttpRequest{
		Method: "patch", URL: h.url("/items/{{ID}}?q=a%20b&tag={{TAG}}&tag=two"),
		BodyType: "json", Body: `{"name":"{{NAME}}"}`,
		RequestHeaders: "X-One: 1\n  X-Two :  two words  \nX-Var: {{TAG}}\nX-Unknown: {{NOPE}}",
	})

	h.mustSend(id, sendOptions{plain: map[string]string{"ID": "42", "TAG": "red", "NAME": "Ada"}})

	got := h.last()
	if got.Method != "PATCH" || got.Path != "/items/42" || got.Query != "q=a%20b&tag=red&tag=two" {
		t.Errorf("server got %s %s?%s", got.Method, got.Path, got.Query)
	}
	if string(got.Body) != `{"name":"Ada"}` {
		t.Errorf("body = %q", got.Body)
	}
	want := map[string]string{"X-One": "1", "X-Two": "two words", "X-Var": "red", "X-Unknown": "{{NOPE}}"}
	for name, value := range want {
		if got.Header.Get(name) != value {
			t.Errorf("%s = %q, want %q", name, got.Header.Get(name), value)
		}
	}
}

func TestTheResponseKeepsWhatWasSentAfterRedirects(t *testing.T) {
	h := newHarness(t)
	id := h.create(models.HttpRequest{Method: "get", URL: h.url("/{{PATH}}")})

	res := h.mustSend(id, sendOptions{plain: map[string]string{"PATH": "moved"}})

	want := h.url("/landed?from=moved")
	for _, saved := range []models.HttpResponse{res, h.history(id)[0]} {
		if saved.Method != "GET" || saved.URL != want {
			t.Errorf("saved %s %s, want GET %s", saved.Method, saved.URL, want)
		}
	}
}

func TestAURLWithoutASchemeIsSentOverHTTP(t *testing.T) {
	h := newHarness(t)
	id := h.create(models.HttpRequest{Method: "GET", URL: strings.TrimPrefix(h.url("/bare"), "http://")})
	if res := h.mustSend(id, sendOptions{}); res.StatusCode != 200 || h.last().Path != "/bare" {
		t.Errorf("status %d, path %q", res.StatusCode, h.last().Path)
	}
}

func TestAFormDataBodyCarriesFieldsAndFiles(t *testing.T) {
	h := newHarness(t)
	upload := filepath.Join(t.TempDir(), "photo.bin")
	content := []byte{0x89, 'P', 'N', 'G', 0x00, 0xff, 0x10}
	if err := os.WriteFile(upload, content, 0o600); err != nil {
		t.Fatal(err)
	}
	id := h.create(models.HttpRequest{
		Method: "POST", URL: h.url("/upload"), BodyType: "form-data",
		BodyForm: fmt.Sprintf(`[
			{"key":"title","type":"text","value":"Hi {{WHO}}","enabled":true},
			{"key":"off","type":"text","value":"x","enabled":false},
			{"key":"avatar","type":"file","file_path":%q,"file_name":"me.png","enabled":true}]`, upload),
	})

	h.mustSend(id, sendOptions{plain: map[string]string{"WHO": "Ada"}})

	got := h.last()
	_, params, err := mime.ParseMediaType(got.Header.Get("Content-Type"))
	if err != nil || params["boundary"] == "" {
		t.Fatalf("Content-Type = %q", got.Header.Get("Content-Type"))
	}
	form, err := multipart.NewReader(bytes.NewReader(got.Body), params["boundary"]).ReadForm(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	if v := form.Value["title"]; len(v) != 1 || v[0] != "Hi Ada" {
		t.Errorf("title = %v", v)
	}
	if _, sent := form.Value["off"]; sent {
		t.Error("a disabled field was sent")
	}
	files := form.File["avatar"]
	if len(files) != 1 || files[0].Filename != "me.png" {
		t.Fatalf("avatar = %+v", files)
	}
	f, _ := files[0].Open()
	defer f.Close()
	var sent bytes.Buffer
	_, _ = sent.ReadFrom(f)
	if !bytes.Equal(sent.Bytes(), content) {
		t.Errorf("file bytes = %v, want %v", sent.Bytes(), content)
	}
}

func TestAMissingUploadFailsWithoutSending(t *testing.T) {
	h := newHarness(t)
	id := h.create(models.HttpRequest{
		Method: "POST", URL: h.url("/upload"), BodyType: "form-data",
		BodyForm: `[{"key":"f","type":"file","file_path":"/no/such/file.png","enabled":true}]`,
	})
	res := h.mustSend(id, sendOptions{})
	if res.StatusCode != 0 || !strings.Contains(res.Body, "file.png") || len(h.requests()) != 0 {
		t.Errorf("response = %d %q, %d sent", res.StatusCode, res.Body, len(h.requests()))
	}
}

func TestRedirectsAreFollowed(t *testing.T) {
	h := newHarness(t)
	target := h.url("/landed")
	hop := httptest.NewServer(http.RedirectHandler(target, http.StatusFound))
	defer hop.Close()
	id := h.create(models.HttpRequest{Method: "GET", URL: hop.URL})
	if res := h.mustSend(id, sendOptions{}); res.StatusCode != 200 || h.last().Path != "/landed" {
		t.Errorf("status %d, landed on %q", res.StatusCode, h.last().Path)
	}
}

func TestErrorStatusesAreSavedAsResponses(t *testing.T) {
	h := newHarness(t)
	for _, code := range []int{204, 404, 500} {
		id := h.create(models.HttpRequest{Method: "GET", URL: h.url(fmt.Sprintf("/status/%d", code))})
		if res := h.mustSend(id, sendOptions{}); res.StatusCode != code {
			t.Errorf("status = %d, want %d", res.StatusCode, code)
		}
	}
}

func TestTheTimeoutSettingStopsASlowServer(t *testing.T) {
	h := newHarness(t)
	h.run("settings.update", map[string]any{"validate_ssl_certificates": true, "request_timeout_seconds": 1}, nil)
	id := h.create(models.HttpRequest{Method: "GET", URL: h.url("/slow?ms=5000")})

	start := time.Now()
	res := h.mustSend(id, sendOptions{})

	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("gave up after %s, want about 1s", elapsed)
	}
	if res.StatusCode != 0 || !strings.Contains(strings.ToLower(res.Body), "timeout") {
		t.Errorf("response = %d %q, want a timeout", res.StatusCode, res.Body)
	}
}

func TestCertificateCheckingFollowsTheSetting(t *testing.T) {
	h := newHarness(t)
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("secure"))
	}))
	defer tlsServer.Close()
	id := h.create(models.HttpRequest{Method: "GET", URL: tlsServer.URL})

	if res := h.mustSend(id, sendOptions{}); res.StatusCode != 0 || !strings.Contains(res.Body, "certificate") {
		t.Errorf("with checking on: %d %q, want a certificate error", res.StatusCode, res.Body)
	}
	h.run("settings.update", map[string]any{"validate_ssl_certificates": false, "request_timeout_seconds": 30}, nil)
	if res := h.mustSend(id, sendOptions{}); res.StatusCode != 200 || res.Body != "secure" {
		t.Errorf("with checking off: %d %q", res.StatusCode, res.Body)
	}
}

func TestALargeBinaryResponseIsSavedIntact(t *testing.T) {
	h := newHarness(t)
	payload := make([]byte, 3<<20)
	_, _ = rand.Read(payload)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	id := h.create(models.HttpRequest{Method: "GET", URL: server.URL})

	res := h.mustSend(id, sendOptions{})

	saved := filepath.Join(t.TempDir(), "download.bin")
	h.run("response.save_to_file", map[string]any{"response_id": res.ID, "file_path": saved}, nil)
	data, err := os.ReadFile(saved)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, payload) {
		t.Errorf("saved %d bytes, differing from the %d sent", len(data), len(payload))
	}
}
