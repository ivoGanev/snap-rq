package selfupdate

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/updater"
)

// SignatureSuffix names a release file's signature: <file>.sig, holding the
// base64 Ed25519 signature of the file's SHA-256 digest.
const SignatureSuffix = ".sig"

//go:embed release_key.pub
var releaseKeyText string

// ReleasePublicKey is the key every update must be signed with. Its private
// half exists only as a secret of the repository's protected "release"
// environment, and offline.
func ReleasePublicKey() (ed25519.PublicKey, error) {
	return ParsePublicKey(releaseKeyText)
}

// ParsePublicKey reads a base64 Ed25519 public key.
func ParsePublicKey(text string) (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(text))
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, errors.New("the release public key is missing or malformed")
	}
	return ed25519.PublicKey(raw), nil
}

// Sign returns the base64 signature for a release file's contents, in the
// form the updater checks: Ed25519 over the SHA-256 digest.
func Sign(key ed25519.PrivateKey, contents []byte) string {
	digest := sha256.Sum256(contents)
	return base64.StdEncoding.EncodeToString(ed25519.Sign(key, digest[:]))
}

// Signed wraps the GitHub provider so that a release is offered only with a
// checksum and a signature; the updater then checks both against the
// download and the pinned public key. The GitHub provider alone installs a
// release that has neither.
func Signed(inner updater.Provider) updater.Provider {
	return signedProvider{inner: inner, client: &http.Client{Timeout: 30 * time.Second}}
}

type signedProvider struct {
	inner  updater.Provider
	client *http.Client
}

func (p signedProvider) Name() string { return p.inner.Name() }

func (p signedProvider) Download(ctx context.Context, r *updater.Release, dst io.Writer, onProgress func(written, total int64)) error {
	return p.inner.Download(ctx, r, dst, onProgress)
}

func (p signedProvider) Check(ctx context.Context, req updater.CheckRequest) (*updater.Release, error) {
	release, err := p.inner.Check(ctx, req)
	if err != nil || release == nil {
		return release, err
	}
	if release.Verification == nil || len(release.Verification.Digest) == 0 {
		return nil, fmt.Errorf("release %s has no checksum for %s", release.Version, release.Artifact.Filename)
	}
	url, _ := release.Metadata["github.asset.url"].(string)
	if url == "" {
		return nil, fmt.Errorf("release %s has no download URL", release.Version)
	}
	signature, err := p.fetchSignature(ctx, url+SignatureSuffix)
	if err != nil {
		return nil, fmt.Errorf("release %s: %w", release.Version, err)
	}
	release.Verification.SignatureAlgo = "ed25519"
	release.Verification.Signature = signature
	return release, nil
}

func (p signedProvider) fetchSignature(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching its signature: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("it is not signed (signature: HTTP %d)", resp.StatusCode)
	}
	text, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if err != nil {
		return nil, err
	}
	signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(text)))
	if err != nil || len(signature) != ed25519.SignatureSize {
		return nil, errors.New("its signature is malformed")
	}
	return signature, nil
}
