package backups

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"snap-rq/backend/backup"
	"snap-rq/backend/commands"
)

// delete.go — delete one backup file. Not undoable: the file is gone. Only
// backup files in the backup folder can be deleted this way.

type Delete struct{ tx *sql.Tx }

func NewDelete(tx *sql.Tx) commands.Command { return &Delete{tx: tx} }

func (Delete) Info() commands.Info {
	return commands.Info{FunctionID: "backup.delete", Label: "Delete Backup"}
}

func (c Delete) Run(input json.RawMessage) (any, error) {
	var in struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	s, err := backup.LoadSettings(c.tx)
	if err != nil {
		return nil, err
	}
	path := filepath.Clean(in.Path)
	if filepath.Dir(path) != filepath.Clean(s.ResolvedFolder()) || !strings.HasSuffix(path, backup.Extension) {
		return nil, errors.New("only backups in the backup folder can be deleted")
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return nil, nil
}
