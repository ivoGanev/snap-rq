package backups

import (
	"database/sql"
	"encoding/json"

	"snap-rq/backend/backup"
	"snap-rq/backend/commands"
)

// list.go — the backup history: the files in the backup folder, newest
// first. Read from the files, not the database, so it works even when the
// database is the thing that's broken.

type List struct{ tx *sql.Tx }

func NewList(tx *sql.Tx) commands.Command { return &List{tx: tx} }

func (List) Info() commands.Info {
	return commands.Info{FunctionID: "backup.list", Label: "List Backups", ReadOnly: true}
}

func (c List) Run(json.RawMessage) (any, error) {
	s, err := backup.LoadSettings(c.tx)
	if err != nil {
		return nil, err
	}
	list, err := backup.List(s.ResolvedFolder())
	if list == nil {
		list = []backup.Info{}
	}
	for i := range list {
		// The sealed keys mean nothing to the page.
		list[i].Header.AppKey = ""
		list[i].Header.PasswordKey = nil
	}
	return list, err
}
