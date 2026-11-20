package backup

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"golang.org/x/crypto/argon2"

	"snap-rq/backend/secrets"
)

// keys.go — the per-backup data key and the two ways it is sealed.

const dataKeySize = 32

// Argon2id settings for the backup password: about a tenth of a second and
// 64 MB, once per backup and once per restore.
const (
	argonTime    = 3
	argonMemory  = 64 * 1024 // KiB
	argonThreads = 4
)

// PasswordKey is the data key sealed with a key derived from the backup
// password; the parameters travel with it so they can change later.
type PasswordKey struct {
	Salt      string `json:"salt"`
	Time      uint32 `json:"time"`
	MemoryKiB uint32 `json:"memory_kib"`
	Threads   uint8  `json:"threads"`
	Sealed    string `json:"sealed"` // nonce ‖ ciphertext, base64
}

func newDataKey() ([]byte, error) {
	key := make([]byte, dataKeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generating the backup key: %w", err)
	}
	return key, nil
}

// sealWithApp seals the data key with the secrets vault and returns it with
// the vault key's fingerprint.
func sealWithApp(q secrets.Querier, dataKey []byte) (sealed, fingerprint string, err error) {
	vault := secrets.Default()
	if sealed, err = vault.Encrypt(q, base64.StdEncoding.EncodeToString(dataKey)); err != nil {
		return "", "", fmt.Errorf("sealing the backup key: %w", err)
	}
	if fingerprint, err = vault.Fingerprint(q); err != nil {
		return "", "", err
	}
	return sealed, fingerprint, nil
}

func sealWithPassword(password string, dataKey []byte) (*PasswordKey, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	sealed, err := gcmSeal(argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, 32), dataKey, nil)
	if err != nil {
		return nil, err
	}
	return &PasswordKey{
		Salt:      base64.StdEncoding.EncodeToString(salt),
		Time:      argonTime,
		MemoryKiB: argonMemory,
		Threads:   argonThreads,
		Sealed:    base64.StdEncoding.EncodeToString(sealed),
	}, nil
}

// Unlock says how to open a backup. The app's own key is tried first when it
// is the one the backup was made with; then a recovery key; then the password.
type Unlock struct {
	RecoveryKey string
	Password    string
}

var (
	// ErrNeedsKey means the backup was made with another key: ask for the
	// recovery key (of that key) or the backup password.
	ErrNeedsKey = errors.New("this backup was made with a different encryption key: enter its recovery key or the backup password")
	// ErrWrongUnlock means what was entered doesn't open this backup.
	ErrWrongUnlock = errors.New("that recovery key or password doesn't open this backup")
)

// dataKeyFor recovers a backup's data key.
func dataKeyFor(q secrets.Querier, h Header, u Unlock) ([]byte, error) {
	if current, err := secrets.Default().Fingerprint(q); err == nil && current == h.KeyFingerprint {
		if plain, err := secrets.Default().Decrypt(q, h.AppKey); err == nil {
			return base64.StdEncoding.DecodeString(plain)
		}
	}
	tried := false
	if u.RecoveryKey != "" {
		tried = true
		if plain, err := secrets.DecryptWithRecoveryKey(u.RecoveryKey, h.AppKey); err == nil {
			return base64.StdEncoding.DecodeString(plain)
		}
	}
	if u.Password != "" && h.PasswordKey != nil {
		tried = true
		if key, err := openWithPassword(u.Password, h.PasswordKey); err == nil {
			return key, nil
		}
	}
	if tried {
		return nil, ErrWrongUnlock
	}
	return nil, ErrNeedsKey
}

func openWithPassword(password string, pk *PasswordKey) ([]byte, error) {
	salt, err := base64.StdEncoding.DecodeString(pk.Salt)
	if err != nil {
		return nil, err
	}
	sealed, err := base64.StdEncoding.DecodeString(pk.Sealed)
	if err != nil {
		return nil, err
	}
	return gcmOpen(argon2.IDKey([]byte(password), salt, pk.Time, pk.MemoryKiB, pk.Threads, 32), sealed, nil)
}

// gcmSeal returns nonce ‖ AES-256-GCM(key, plain), authenticating extra.
func gcmSeal(key, plain, extra []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plain, extra), nil
}

func gcmOpen(key, sealed, extra []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize() {
		return nil, errors.New("the backup is damaged")
	}
	return gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], extra)
}
