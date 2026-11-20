package backup

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"snap-rq/backend/secrets"
)

// read.go — listing backups and turning one back into a database file.

// List returns the backups in dir, newest first. Files that aren't readable
// backups are skipped; leftovers of an interrupted backup older than an hour
// are removed.
func List(dir string) ([]Info, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var list []Info
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if strings.HasPrefix(e.Name(), tempPrefix) {
			if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > time.Hour {
				os.Remove(path)
			}
			continue
		}
		if !strings.HasSuffix(e.Name(), Extension) {
			continue
		}
		h, err := ReadHeader(path)
		if err != nil {
			continue
		}
		size := int64(0)
		if info, err := e.Info(); err == nil {
			size = info.Size()
		}
		list = append(list, Info{Path: path, Name: e.Name(), Bytes: size, Header: h})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Header.CreatedAt.After(list[j].Header.CreatedAt) })
	return list, nil
}

// Extract decrypts a backup into a SQLite file at dest and checks it. q is
// the current database, for the app's own key.
func Extract(path, dest string, q secrets.Querier, u Unlock) (Header, error) {
	f, err := os.Open(path)
	if err != nil {
		return Header{}, err
	}
	defer f.Close()
	r := bufio.NewReader(f)
	h, headerJSON, err := readHeader(r)
	if err != nil {
		return Header{}, err
	}
	payload, err := io.ReadAll(r)
	if err != nil {
		return Header{}, err
	}

	dataKey, err := dataKeyFor(q, h, u)
	if err != nil {
		return h, err
	}
	compressed, err := gcmOpen(dataKey, payload, headerJSON)
	if err != nil {
		return h, errDamaged
	}
	zr, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return h, errDamaged
	}
	database, err := io.ReadAll(zr)
	if err != nil {
		return h, errDamaged
	}

	if err := os.WriteFile(dest, database, 0o600); err != nil {
		return h, err
	}
	if err := quickCheck(dest); err != nil {
		os.Remove(dest)
		return h, err
	}
	return h, nil
}

var errDamaged = errors.New("the backup is damaged or was changed")
