package backup

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "modernc.org/sqlite" // the snapshot is checked with its own connection
)

// create.go — making a backup in two parts. Begin is what must happen before
// a risky change: a consistent copy of the database (VACUUM INTO, about a
// third of a second for 50 MB, without blocking other writers in WAL mode)
// and a sealed data key. Finish compresses, encrypts and writes the file, and
// can run in the background.

// Options describe the backup to make.
type Options struct {
	Dir        string
	Kind       Kind
	Reason     string // e.g. "before Delete collection Auth"
	AppVersion string
	Password   string // optional backup password
}

// Pending is a backup that has its snapshot and key but no file yet.
type Pending struct {
	opts     Options
	snapshot string
	dataKey  []byte
	header   Header
}

// Info describes a backup file.
type Info struct {
	Path   string `json:"path"`
	Name   string `json:"name"`
	Bytes  int64  `json:"bytes"`
	Header Header `json:"header"`
}

// Begin takes the snapshot and seals a fresh data key.
func Begin(db *sql.DB, opts Options) (*Pending, error) {
	if opts.Dir == "" {
		return nil, errors.New("no backup folder")
	}
	if err := os.MkdirAll(opts.Dir, 0o700); err != nil {
		return nil, fmt.Errorf("creating the backup folder: %w", err)
	}

	snapshot := filepath.Join(opts.Dir, tempName(".db"))
	if _, err := db.Exec(`VACUUM INTO ?`, snapshot); err != nil {
		os.Remove(snapshot)
		return nil, fmt.Errorf("copying the database: %w", err)
	}
	if err := quickCheck(snapshot); err != nil {
		os.Remove(snapshot)
		return nil, err
	}
	stat, err := os.Stat(snapshot)
	if err != nil {
		os.Remove(snapshot)
		return nil, err
	}

	dataKey, err := newDataKey()
	if err != nil {
		os.Remove(snapshot)
		return nil, err
	}
	header := Header{
		Format:        FormatVersion,
		CreatedAt:     time.Now().UTC().Truncate(time.Second),
		Kind:          opts.Kind,
		Reason:        opts.Reason,
		AppVersion:    opts.AppVersion,
		DatabaseBytes: stat.Size(),
	}
	if header.AppKey, header.KeyFingerprint, err = sealWithApp(db, dataKey); err != nil {
		os.Remove(snapshot)
		return nil, err
	}
	if opts.Password != "" {
		if header.PasswordKey, err = sealWithPassword(opts.Password, dataKey); err != nil {
			os.Remove(snapshot)
			return nil, err
		}
	}
	return &Pending{opts: opts, snapshot: snapshot, dataKey: dataKey, header: header}, nil
}

// Finish compresses and encrypts the snapshot into the backup file, then
// removes the snapshot. The file appears under its final name only once it
// is complete and on disk.
func (p *Pending) Finish() (Info, error) {
	defer os.Remove(p.snapshot)

	raw, err := os.ReadFile(p.snapshot)
	if err != nil {
		return Info{}, err
	}
	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	if _, err := zw.Write(raw); err != nil {
		return Info{}, err
	}
	if err := zw.Close(); err != nil {
		return Info{}, err
	}

	headerJSON, err := json.Marshal(p.header)
	if err != nil {
		return Info{}, err
	}
	payload, err := gcmSeal(p.dataKey, compressed.Bytes(), headerJSON)
	if err != nil {
		return Info{}, err
	}

	temp := filepath.Join(p.opts.Dir, tempName(Extension))
	if err := writeFile(temp, headerJSON, payload); err != nil {
		os.Remove(temp)
		return Info{}, fmt.Errorf("writing the backup: %w", err)
	}
	final := uniquePath(p.opts.Dir, fileName(p.header))
	if err := os.Rename(temp, final); err != nil {
		os.Remove(temp)
		return Info{}, fmt.Errorf("saving the backup: %w", err)
	}
	return Info{Path: final, Name: filepath.Base(final), Bytes: int64(len(headerJSON) + len(payload) + len(magic) + 4), Header: p.header}, nil
}

// Discard drops a pending backup (the change it guarded didn't happen).
func (p *Pending) Discard() { os.Remove(p.snapshot) }

// Create makes a whole backup at once.
func Create(db *sql.DB, opts Options) (Info, error) {
	p, err := Begin(db, opts)
	if err != nil {
		return Info{}, err
	}
	return p.Finish()
}

// tempPrefix marks files that are not finished backups; List ignores them
// and removes leftovers from a crash.
const tempPrefix = ".pending-"

func tempName(ext string) string {
	b := make([]byte, 6)
	rand.Read(b)
	return tempPrefix + hex.EncodeToString(b) + ext
}

var unsafeChars = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]+`)

// fileName is "2026-11-20 15-30-05 – before Delete collection Auth.snaprq-backup",
// in local time, with characters no OS allows in names removed.
func fileName(h Header) string {
	reason := strings.TrimSpace(unsafeChars.ReplaceAllString(h.Reason, " "))
	if len([]rune(reason)) > 80 {
		reason = string([]rune(reason)[:80])
	}
	name := h.CreatedAt.Local().Format("2006-01-02 15-04-05")
	if reason != "" {
		name += " – " + reason
	}
	return name + Extension
}

// uniquePath adds " (2)", " (3)"… when a name is taken.
func uniquePath(dir, name string) string {
	path := filepath.Join(dir, name)
	base := strings.TrimSuffix(name, Extension)
	for i := 2; ; i++ {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return path
		}
		path = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, i, Extension))
	}
}

// quickCheck opens a SQLite file and runs PRAGMA quick_check.
func quickCheck(path string) error {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return err
	}
	defer db.Close()
	var result string
	if err := db.QueryRow(`PRAGMA quick_check`).Scan(&result); err != nil {
		return fmt.Errorf("checking the copy: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("the copy failed its check: %s", result)
	}
	return nil
}
