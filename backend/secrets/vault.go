// Package secrets encrypts the values of secret environment variables.
//
// One random AES-256 key encrypts every secret value. The key lives in a
// KeyStore (the OS credential store), never in the database, and is read once
// per session. A key check, a fixed text encrypted with the key and kept in
// app_settings, tells "first use" from "key missing" and "wrong key", so a new
// key is never created while values encrypted with another one exist.
// See docs/plans/secret-variables.md.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	keySize       = 32
	encodedPrefix = "v1:"
	checkSetting  = "secrets_key_check"
	checkText     = "snaprq-key-check"
	recoveryLabel = "SNRQ"
)

var (
	// ErrLocked means secret values exist but the key that encrypted them is
	// missing or different. Nothing is overwritten until the user restores
	// the recovery key or starts over.
	ErrLocked = errors.New("secret values are locked: the encryption key is missing on this computer")
	// ErrWrongRecoveryKey means a recovery key doesn't decrypt this database.
	ErrWrongRecoveryKey = errors.New("that recovery key doesn't match these secrets")
)

// Querier is what the vault needs from the database: the caller's
// transaction. SQLite has one writer, so using a separate connection while a
// command's transaction is open would deadlock.
type Querier interface {
	QueryRow(query string, args ...any) *sql.Row
	Exec(query string, args ...any) (sql.Result, error)
}

// State is what Status reports.
type State string

const (
	StateNone     State = "none"     // no secret has been encrypted yet
	StateUnlocked State = "unlocked" // the key is here and matches
	StateLocked   State = "locked"   // secrets exist but the key is missing or wrong
)

// Vault encrypts and decrypts secret values.
type Vault struct {
	store KeyStore
	mu    sync.Mutex
	key   []byte // cached after the first successful unlock
}

// NewVault returns a vault backed by store.
func NewVault(store KeyStore) *Vault { return &Vault{store: store} }

var current atomic.Pointer[Vault]

// SetDefault sets the vault commands use. main sets the OS-backed one.
func SetDefault(v *Vault) { current.Store(v) }

// Default is the vault commands use. Until SetDefault is called (tests) it is
// an in-memory vault.
func Default() *Vault {
	if v := current.Load(); v != nil {
		return v
	}
	current.CompareAndSwap(nil, NewVault(MemoryKeyStore()))
	return current.Load()
}

// StoreKind says where the key lives (StoreKeyring, StoreFile, StoreMemory).
func (v *Vault) StoreKind() string { return v.store.Kind() }

// IsEncrypted reports whether a stored value is in the vault's format.
func IsEncrypted(stored string) bool { return strings.HasPrefix(stored, encodedPrefix) }

// Encrypt encrypts a value, creating the key on first use.
func (v *Vault) Encrypt(q Querier, plain string) (string, error) {
	key, err := v.unlock(q, true)
	if err != nil {
		return "", err
	}
	return seal(key, plain)
}

// Decrypt decrypts a value produced by Encrypt.
func (v *Vault) Decrypt(q Querier, stored string) (string, error) {
	key, err := v.unlock(q, false)
	if err != nil {
		return "", err
	}
	return open(key, stored)
}

// Status reports whether secrets can be used, without creating a key.
func (v *Vault) Status(q Querier) (State, error) {
	check, err := readCheck(q)
	if err != nil {
		return "", err
	}
	if check == "" {
		return StateNone, nil
	}
	if _, err := v.unlock(q, false); err != nil {
		if errors.Is(err, ErrLocked) {
			return StateLocked, nil
		}
		return "", err
	}
	return StateUnlocked, nil
}

// RecoveryKey returns the key in its readable form (SNRQ-XXXX-…).
func (v *Vault) RecoveryKey(q Querier) (string, error) {
	key, err := v.unlock(q, false)
	if err != nil {
		return "", err
	}
	return formatRecoveryKey(key), nil
}

// Restore puts a recovery key back into the key store, after checking that
// it decrypts this database's secrets.
func (v *Vault) Restore(q Querier, recoveryKey string) error {
	key, err := parseRecoveryKey(recoveryKey)
	if err != nil {
		return err
	}
	check, err := readCheck(q)
	if err != nil {
		return err
	}
	if check != "" {
		if text, err := open(key, check); err != nil || text != checkText {
			return ErrWrongRecoveryKey
		}
	} else if err := writeCheck(q, key); err != nil {
		return err
	}
	if err := v.store.Save(key); err != nil {
		return err
	}
	v.mu.Lock()
	v.key = key
	v.mu.Unlock()
	return nil
}

// Forget drops the key check (the caller clears the secret values in the
// same transaction), so the next Encrypt starts with a working key: the one
// already in the store, or a new one.
func (v *Vault) Forget(q Querier) error {
	if _, err := q.Exec(`DELETE FROM app_settings WHERE key = ?`, checkSetting); err != nil {
		return fmt.Errorf("clearing the key check: %w", err)
	}
	v.mu.Lock()
	v.key = nil
	v.mu.Unlock()
	return nil
}

// unlock returns the key, loading and checking it on first use. With create
// it makes a key when none exists and nothing has been encrypted yet.
func (v *Vault) unlock(q Querier, create bool) ([]byte, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.key != nil {
		return v.key, nil
	}

	check, err := readCheck(q)
	if err != nil {
		return nil, err
	}
	key, err := v.store.Load()
	switch {
	case errors.Is(err, ErrNoKey):
		// Never replace a lost key while values encrypted with it exist.
		if check != "" || !create {
			return nil, ErrLocked
		}
		if key, err = newKey(); err != nil {
			return nil, err
		}
		if err := v.store.Save(key); err != nil {
			return nil, err
		}
	case err != nil:
		return nil, err
	}

	if check == "" {
		if err := writeCheck(q, key); err != nil {
			return nil, err
		}
	} else if text, err := open(key, check); err != nil || text != checkText {
		return nil, ErrLocked // a key from another database
	}
	v.key = key
	return key, nil
}

func newKey() ([]byte, error) {
	key := make([]byte, keySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generating the encryption key: %w", err)
	}
	return key, nil
}

func readCheck(q Querier) (string, error) {
	var raw string
	err := q.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, checkSetting).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading the key check: %w", err)
	}
	var check string
	if err := json.Unmarshal([]byte(raw), &check); err != nil {
		return "", fmt.Errorf("reading the key check: %w", err)
	}
	return check, nil
}

func writeCheck(q Querier, key []byte) error {
	sealed, err := seal(key, checkText)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(sealed)
	_, err = q.Exec(`INSERT INTO app_settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, checkSetting, string(raw))
	if err != nil {
		return fmt.Errorf("writing the key check: %w", err)
	}
	return nil
}

// seal encrypts with AES-256-GCM: "v1:" + base64(nonce ‖ ciphertext).
func seal(key []byte, plain string) (string, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return encodedPrefix + base64.RawStdEncoding.EncodeToString(sealed), nil
}

func open(key []byte, stored string) (string, error) {
	if !IsEncrypted(stored) {
		return "", errors.New("value is not encrypted")
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(stored, encodedPrefix))
	if err != nil {
		return "", errors.New("encrypted value is damaged")
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("encrypted value is damaged")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", errors.New("encrypted value can't be decrypted with this key")
	}
	return string(plain), nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

var recoveryEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// formatRecoveryKey shows the key as SNRQ-XXXX-XXXX-… for reading and typing.
func formatRecoveryKey(key []byte) string {
	encoded := recoveryEncoding.EncodeToString(key)
	groups := []string{recoveryLabel}
	for i := 0; i < len(encoded); i += 4 {
		groups = append(groups, encoded[i:min(i+4, len(encoded))])
	}
	return strings.Join(groups, "-")
}

// parseRecoveryKey accepts the key with or without the label, dashes and
// spaces, in any case.
func parseRecoveryKey(text string) ([]byte, error) {
	cleaned := strings.ToUpper(strings.Join(strings.Fields(text), ""))
	cleaned = strings.TrimPrefix(strings.ReplaceAll(cleaned, "-", ""), recoveryLabel)
	key, err := recoveryEncoding.DecodeString(cleaned)
	if err != nil || len(key) != keySize {
		return nil, errors.New("that doesn't look like a SnapRQ recovery key")
	}
	return key, nil
}
