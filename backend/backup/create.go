package backup

import (
	"bytes"
	"compress/gzip"
	"context"
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
	"sync"
	"time"

	sqlite "modernc.org/sqlite"

	"snap-rq/backend/secrets"
)

// create.go — making a backup in two parts.
//
// Begin is all that happens before a risky change, and takes a millisecond or
// two: it freezes the database by opening a read transaction on a connection
// of its own (in WAL mode that connection keeps seeing this state, whatever is
// committed afterwards) and seals a fresh data key.
//
// Finish does the heavy work, normally in the background after the change has
// committed: it copies the frozen snapshot with SQLite's online backup API,
// compacts the copy (VACUUM, which also drops leftovers of deleted data from
// free pages), checks it, then compresses, encrypts and writes the file.
// Finishes run one at a time: snapshots are instant, copies queue up.

// Options describe the backup to make.
type Options struct {
	Dir        string
	Kind       Kind
	Reason     string // e.g. "before Delete collection Auth"
	AppVersion string
	Password   string // optional backup password
}

// Pending is a backup whose snapshot is frozen but not yet written.
type Pending struct {
	opts    Options
	conn    *sql.Conn // holds the read transaction: the frozen snapshot
	dataKey []byte
	header  Header
	once    sync.Once
}

// Info describes a backup file.
type Info struct {
	Path   string `json:"path"`
	Name   string `json:"name"`
	Bytes  int64  `json:"bytes"`
	Header Header `json:"header"`
}

// work serialises Finish: one copy, compression and encryption at a time.
var work sync.Mutex

// Begin freezes the database as it is now and seals a fresh data key.
func Begin(db *sql.DB, opts Options) (*Pending, error) {
	if opts.Dir == "" {
		return nil, errors.New("no backup folder")
	}
	if err := os.MkdirAll(opts.Dir, 0o700); err != nil {
		return nil, fmt.Errorf("creating the backup folder: %w", err)
	}

	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	p := &Pending{opts: opts, conn: conn}
	if _, err := conn.ExecContext(ctx, `BEGIN`); err != nil {
		p.release()
		return nil, fmt.Errorf("freezing the database: %w", err)
	}
	// A read starts the transaction, and with it the snapshot.
	var one int
	if err := conn.QueryRowContext(ctx, `SELECT 1 FROM sqlite_master LIMIT 1`).Scan(&one); err != nil && !errors.Is(err, sql.ErrNoRows) {
		p.release()
		return nil, fmt.Errorf("freezing the database: %w", err)
	}

	if p.dataKey, err = newDataKey(); err != nil {
		p.release()
		return nil, err
	}
	p.header = Header{
		Format:     FormatVersion,
		CreatedAt:  time.Now().UTC().Truncate(time.Second),
		Kind:       opts.Kind,
		Reason:     opts.Reason,
		AppVersion: opts.AppVersion,
	}
	p.header.AppKey, p.header.KeyFingerprint, err = sealWithApp(db, p.dataKey)
	if err != nil && !(errors.Is(err, secrets.ErrLocked) && opts.Password != "") {
		// With the key locked, only a password can seal the backup.
		p.release()
		return nil, err
	}
	if opts.Password != "" {
		if p.header.PasswordKey, err = sealWithPassword(opts.Password, p.dataKey); err != nil {
			p.release()
			return nil, err
		}
	}
	return p, nil
}

// Finish copies the frozen snapshot and writes the backup file. The file
// appears under its final name only once it is complete and on disk.
func (p *Pending) Finish() (Info, error) {
	work.Lock()
	defer work.Unlock()

	copyPath := filepath.Join(p.opts.Dir, tempName(".db"))
	defer os.Remove(copyPath)
	err := copySnapshot(p.conn, copyPath)
	p.release() // the snapshot is no longer needed
	if err != nil {
		return Info{}, fmt.Errorf("copying the database: %w", err)
	}
	if err := compact(copyPath); err != nil {
		return Info{}, err
	}
	if err := quickCheck(copyPath); err != nil {
		return Info{}, err
	}

	raw, err := os.ReadFile(copyPath)
	if err != nil {
		return Info{}, err
	}
	p.header.DatabaseBytes = int64(len(raw))
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
func (p *Pending) Discard() { p.release() }

// release ends the read transaction and returns the connection to the pool.
func (p *Pending) release() {
	p.once.Do(func() {
		_, _ = p.conn.ExecContext(context.Background(), `ROLLBACK`)
		p.conn.Close()
	})
}

// backuper is the modernc driver connection's online backup API.
type backuper interface {
	NewBackup(dstURI string) (*sqlite.Backup, error)
}

// copySnapshot copies the database as conn's open read transaction sees it.
func copySnapshot(conn *sql.Conn, dest string) error {
	return conn.Raw(func(dc any) error {
		b, ok := dc.(backuper)
		if !ok {
			return errors.New("the SQLite driver can't make online backups")
		}
		bk, err := b.NewBackup(dest)
		if err != nil {
			return err
		}
		if _, err := bk.Step(-1); err != nil { // -1: every page at once
			bk.Finish()
			return err
		}
		return bk.Finish()
	})
}

// compact rewrites a copied database without free pages: smaller, and without
// leftovers of deleted rows.
func compact(path string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec(`VACUUM`); err != nil {
		return fmt.Errorf("compacting the copy: %w", err)
	}
	return nil
}

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
	reason := strings.ReplaceAll(h.Reason, `"`, "") // collection "Auth" → collection Auth
	reason = strings.Join(strings.Fields(unsafeChars.ReplaceAllString(reason, " ")), " ")
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
