// Package backup makes and reads backups: whole-database snapshots,
// compressed and encrypted, one file each. See docs/plans/backups.md.
//
// A backup file is
//
//	"SNRQBAK1" | uint32 header length (big endian) | header JSON | payload
//
// The payload is AES-256-GCM(data key, gzip(SQLite file)) as nonce ‖
// ciphertext, with the header bytes as additional data, so a changed header
// is detected too. Every backup has its own random data key, kept in the
// header twice at most: sealed with the app's secrets key (always) and with
// a key derived from the backup password (when one is set). Either opens it.
package backup

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

const (
	magic = "SNRQBAK1"
	// FormatVersion is the newest format this build reads and writes.
	FormatVersion = 1
	// Extension of backup files.
	Extension = ".snaprq-backup"
	// maxHeaderSize guards against reading a damaged length.
	maxHeaderSize = 1 << 20
)

// Kind says who asked for a backup.
type Kind string

const (
	Automatic Kind = "automatic" // before a risky operation or an update; rotated
	Manual    Kind = "manual"    // "Back up now"; never deleted automatically
)

// Header is the readable part of a backup: what it is, and the sealed data
// key. The reason is readable on purpose, so the history can be listed
// without unlocking anything.
type Header struct {
	Format     int       `json:"format"`
	CreatedAt  time.Time `json:"created_at"`
	Kind       Kind      `json:"kind"`
	Reason     string    `json:"reason"`
	AppVersion string    `json:"app_version"`
	// DatabaseBytes is the size of the restored SQLite file.
	DatabaseBytes int64 `json:"database_bytes"`
	// KeyFingerprint names the secrets key the data key is sealed with
	// (secrets.Vault.Fingerprint): it tells which recovery key opens it.
	KeyFingerprint string `json:"key_fingerprint"`
	// AppKey is the data key sealed by the secrets vault ("v1:…").
	AppKey string `json:"app_key"`
	// PasswordKey is the data key sealed with the backup password, if set.
	PasswordKey *PasswordKey `json:"password_key,omitempty"`
}

// writeFile writes magic, header and payload to path.
func writeFile(path string, headerJSON, payload []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(headerJSON)))
	for _, part := range [][]byte{[]byte(magic), length[:], headerJSON, payload} {
		if _, err := w.Write(part); err != nil {
			f.Close()
			return err
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	// The file must be on disk before it is renamed into place.
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// readHeader reads the header of an open backup file and returns it with
// its raw bytes (the payload's additional data). The reader is left at the
// start of the payload.
func readHeader(r io.Reader) (Header, []byte, error) {
	var start [len(magic) + 4]byte
	if _, err := io.ReadFull(r, start[:]); err != nil || string(start[:len(magic)]) != magic {
		return Header{}, nil, errors.New("not a SnapRQ backup")
	}
	size := binary.BigEndian.Uint32(start[len(magic):])
	if size == 0 || size > maxHeaderSize {
		return Header{}, nil, errors.New("the backup is damaged")
	}
	raw := make([]byte, size)
	if _, err := io.ReadFull(r, raw); err != nil {
		return Header{}, nil, errors.New("the backup is damaged")
	}
	var h Header
	if err := json.Unmarshal(raw, &h); err != nil {
		return Header{}, nil, errors.New("the backup is damaged")
	}
	if h.Format > FormatVersion {
		return Header{}, nil, fmt.Errorf("this backup was made by a newer SnapRQ (format %d); update SnapRQ to restore it", h.Format)
	}
	return h, raw, nil
}

// ReadHeader reads just the header of a backup file.
func ReadHeader(path string) (Header, error) {
	f, err := os.Open(path)
	if err != nil {
		return Header{}, err
	}
	defer f.Close()
	h, _, err := readHeader(bufio.NewReader(f))
	return h, err
}
