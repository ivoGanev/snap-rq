package selfupdate

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/updater"
)

// fakeGitHub stands in for the GitHub provider: it offers one release whose
// file is served next to its signature.
type fakeGitHub struct {
	release *updater.Release
}

func (fakeGitHub) Name() string { return "github" }

func (f fakeGitHub) Check(context.Context, updater.CheckRequest) (*updater.Release, error) {
	return f.release, nil
}

func (fakeGitHub) Download(context.Context, *updater.Release, io.Writer, func(int64, int64)) error {
	return nil
}

func release(server *httptest.Server, digest []byte) *updater.Release {
	r := &updater.Release{
		Version:  "0.2.0",
		Artifact: updater.Artifact{Filename: "app.zip"},
		Metadata: map[string]any{"github.asset.url": server.URL + "/app.zip"},
	}
	if digest != nil {
		r.Verification = &updater.Verification{DigestAlgo: "sha256", Digest: digest}
	}
	return r
}

func serveSignature(t *testing.T, signature string, status int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/app.zip.sig" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(signature))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestASignedReleaseCarriesASignatureTheUpdaterAccepts(t *testing.T) {
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	contents := []byte("the update")
	digest := sha256.Sum256(contents)
	server := serveSignature(t, Sign(private, contents)+"\n", http.StatusOK)

	got, err := Signed(fakeGitHub{release(server, digest[:])}).Check(context.Background(), updater.CheckRequest{})
	if err != nil {
		t.Fatal(err)
	}
	v := got.Verification
	if v.SignatureAlgo != "ed25519" {
		t.Fatalf("signature algorithm = %q", v.SignatureAlgo)
	}
	// The updater's ed25519 check: the signature over the downloaded digest.
	if !ed25519.Verify(public, digest[:], v.Signature) {
		t.Error("the signature doesn't verify over the file's digest")
	}
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	if ed25519.Verify(other, digest[:], v.Signature) {
		t.Error("the signature verifies under a different key")
	}
}

func TestAReleaseWithoutChecksumOrSignatureIsRefused(t *testing.T) {
	_, private, _ := ed25519.GenerateKey(rand.Reader)
	digest := sha256.Sum256([]byte("x"))
	cases := []struct {
		name    string
		server  *httptest.Server
		digest  []byte
		message string
	}{
		{"no checksum", serveSignature(t, Sign(private, []byte("x")), http.StatusOK), nil, "no checksum"},
		{"no signature", serveSignature(t, "", http.StatusNotFound), digest[:], "not signed"},
		{"garbled signature", serveSignature(t, "not base64!", http.StatusOK), digest[:], "malformed"},
		{"short signature", serveSignature(t, base64.StdEncoding.EncodeToString([]byte("short")), http.StatusOK), digest[:], "malformed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Signed(fakeGitHub{release(c.server, c.digest)}).Check(context.Background(), updater.CheckRequest{})
			if err == nil || !strings.Contains(err.Error(), c.message) {
				t.Errorf("Check = %v, %v; want an error containing %q", got, err, c.message)
			}
		})
	}
}

func TestUpToDateStaysUpToDate(t *testing.T) {
	got, err := Signed(fakeGitHub{}).Check(context.Background(), updater.CheckRequest{})
	if got != nil || err != nil {
		t.Errorf("Check = %v, %v; want nothing to update", got, err)
	}
}

func TestTheAppCarriesARealReleaseKey(t *testing.T) {
	if _, err := ReleasePublicKey(); err != nil {
		t.Fatalf("%v: run `go run ./tools/releasesign keygen <private-key-file>`", err)
	}
}
