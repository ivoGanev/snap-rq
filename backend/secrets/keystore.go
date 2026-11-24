package secrets

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"snap-rq/backend/identity"

	"github.com/zalando/go-keyring"
)

// keystore.go — where the encryption key lives. The key is never stored in
// the database; a copy of app.db alone cannot decrypt anything.

// ErrNoKey means the store holds no key (yet).
var ErrNoKey = errors.New("no encryption key stored")

// Where a KeyStore keeps the key, as shown in Settings.
const (
	StoreKeyring = "keyring" // the OS credential store
	StoreFile    = "file"    // secret.key next to the database (no keyring available)
	StoreMemory  = "memory"  // tests
)

// KeyStore loads and saves the one encryption key.
type KeyStore interface {
	Load() ([]byte, error) // ErrNoKey when there is none
	Save(key []byte) error
	Kind() string
}

const (
	keyringService = identity.KeyringService
	keyringAccount = identity.KeyringAccount
)

// OSKeyStore uses the OS credential store (Windows Credential Manager, macOS
// Keychain, Linux Secret Service). When the OS has none, for example Linux
// without a keyring service, it falls back to a file readable only by the
// user at fallbackPath.
func OSKeyStore(fallbackPath string) KeyStore {
	return &osKeyStore{fallback: fileKeyStore{path: fallbackPath}}
}

type osKeyStore struct {
	once     sync.Once
	resolved KeyStore
	fallback fileKeyStore
}

// store picks the keyring if it answers, the file otherwise. Decided once:
// probing is a call to the OS, and the answer doesn't change while running.
func (s *osKeyStore) store() KeyStore {
	s.once.Do(func() {
		_, err := keyring.Get(keyringService, keyringAccount)
		if err == nil || errors.Is(err, keyring.ErrNotFound) {
			s.resolved = keyringKeyStore{}
			return
		}
		s.resolved = s.fallback
	})
	return s.resolved
}

func (s *osKeyStore) Load() ([]byte, error) { return s.store().Load() }
func (s *osKeyStore) Save(key []byte) error { return s.store().Save(key) }
func (s *osKeyStore) Kind() string          { return s.store().Kind() }

type keyringKeyStore struct{}

func (keyringKeyStore) Load() ([]byte, error) {
	encoded, err := keyring.Get(keyringService, keyringAccount)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil, ErrNoKey
	}
	if err != nil {
		return nil, fmt.Errorf("reading the key from the credential store: %w", err)
	}
	return decodeKey(encoded)
}

func (keyringKeyStore) Save(key []byte) error {
	if err := keyring.Set(keyringService, keyringAccount, base64.StdEncoding.EncodeToString(key)); err != nil {
		return fmt.Errorf("saving the key to the credential store: %w", err)
	}
	return nil
}

func (keyringKeyStore) Kind() string { return StoreKeyring }

// FileKeyStore keeps the key in a file readable only by the user, e.g. for
// a data folder that must not share the OS credential store's key.
func FileKeyStore(path string) KeyStore { return fileKeyStore{path: path} }

type fileKeyStore struct{ path string }

func (s fileKeyStore) Load() ([]byte, error) {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoKey
	}
	if err != nil {
		return nil, fmt.Errorf("reading the key file: %w", err)
	}
	return decodeKey(strings.TrimSpace(string(raw)))
}

func (s fileKeyStore) Save(key []byte) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("creating the key folder: %w", err)
	}
	if err := os.WriteFile(s.path, []byte(base64.StdEncoding.EncodeToString(key)+"\n"), 0o600); err != nil {
		return fmt.Errorf("writing the key file: %w", err)
	}
	return nil
}

func (fileKeyStore) Kind() string { return StoreFile }

// MemoryKeyStore keeps the key in memory only, for tests.
func MemoryKeyStore() KeyStore { return &memoryKeyStore{} }

type memoryKeyStore struct {
	mu  sync.Mutex
	key []byte
}

func (s *memoryKeyStore) Load() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.key == nil {
		return nil, ErrNoKey
	}
	return append([]byte(nil), s.key...), nil
}

func (s *memoryKeyStore) Save(key []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.key = append([]byte(nil), key...)
	return nil
}

func (*memoryKeyStore) Kind() string { return StoreMemory }

func decodeKey(encoded string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) != keySize {
		return nil, errors.New("the stored encryption key is damaged")
	}
	return key, nil
}
