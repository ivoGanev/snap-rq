package backup

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// restore.go — putting a backup back. The open database can't be replaced
// while the app runs, so restoring has two halves:
//
//  1. PrepareRestore (app running): decrypt and check the backup into a
//     pending file next to the database, take a "before restore" backup of
//     the current one, and leave a marker.
//  2. ApplyPendingRestore (next start, before the database is opened): swap
//     the pending file in. Migrations then run as on any start, so backups
//     from older versions work.

const (
	pendingFile = "restore-pending.db"
	markerFile  = "restore-pending.json"
)

// ErrNewerBackup means the backup was made by a newer SnapRQ.
var ErrNewerBackup = errors.New("this backup was made by a newer version of SnapRQ: update SnapRQ to restore it")

type marker struct {
	Backup    string    `json:"backup"`
	CreatedAt time.Time `json:"created_at"`
}

// PrepareRestore readies a restore of the backup at path for the next start.
func PrepareRestore(db *sql.DB, dataDir, path string, u Unlock) (Header, error) {
	h, err := ReadHeader(path)
	if err != nil {
		return Header{}, err
	}
	if newerVersion(h.AppVersion, AppVersion) {
		return h, ErrNewerBackup
	}

	pending := filepath.Join(dataDir, pendingFile)
	os.Remove(pending)
	if _, err := Extract(path, pending, db, u); err != nil {
		os.Remove(pending)
		return h, err
	}

	// The restore can itself be undone: keep the current database first.
	if _, err := Take(db, Manual, "before restore of "+strings.TrimSuffix(filepath.Base(path), Extension)); err != nil {
		os.Remove(pending)
		return h, fmt.Errorf("nothing was changed: the backup of the current data failed: %w", err)
	}

	raw, _ := json.Marshal(marker{Backup: filepath.Base(path), CreatedAt: h.CreatedAt})
	if err := os.WriteFile(filepath.Join(dataDir, markerFile), raw, 0o600); err != nil {
		os.Remove(pending)
		return h, err
	}
	return h, nil
}

// CancelRestore drops a prepared restore that hasn't been applied.
func CancelRestore(dataDir string) {
	os.Remove(filepath.Join(dataDir, markerFile))
	os.Remove(filepath.Join(dataDir, pendingFile))
}

// ApplyPendingRestore swaps a prepared restore in, before the database is
// opened. It returns the restored backup's file name ("" when there was
// none). The old process may still be closing the database (a restart), so
// the swap retries for a few seconds.
func ApplyPendingRestore(dataDir, dbPath string) (string, error) {
	markerPath := filepath.Join(dataDir, markerFile)
	raw, err := os.ReadFile(markerPath)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var m marker
	_ = json.Unmarshal(raw, &m)
	pending := filepath.Join(dataDir, pendingFile)
	if _, err := os.Stat(pending); err != nil {
		os.Remove(markerPath) // nothing to apply
		return "", nil
	}

	var lastErr error
	for attempt := 0; attempt < 50; attempt++ {
		if lastErr = swapIn(pending, dbPath); lastErr == nil {
			os.Remove(markerPath)
			return m.Backup, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return "", fmt.Errorf("restoring %s: %w", m.Backup, lastErr)
}

// swapIn replaces the database with the restored file. The old WAL and
// shared-memory files belong to the old database and must go: their content
// is in the "before restore" backup.
func swapIn(pending, dbPath string) error {
	for _, extra := range []string{dbPath + "-wal", dbPath + "-shm"} {
		if err := os.Remove(extra); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return os.Rename(pending, dbPath)
}

// newerVersion reports whether a is a newer release than b. Development
// builds (anything that isn't a plain x.y.z) never count as newer or older.
func newerVersion(a, b string) bool {
	pa, okA := parseVersion(a)
	pb, okB := parseVersion(b)
	if !okA || !okB {
		return false
	}
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
