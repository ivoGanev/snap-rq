package backups

import (
	"database/sql"
	"encoding/json"

	"snap-rq/backend/backup"
	"snap-rq/backend/commands"
)

// create.go — "Back up now". Observable, like request.send: a snapshot needs
// the database itself (VACUUM INTO can't run inside a transaction), and it
// reports when the file is written.

type Create struct{ db *sql.DB }

func NewCreate(db *sql.DB) commands.ObservableCommand { return &Create{db: db} }

func (Create) Info() commands.Info {
	return commands.Info{FunctionID: "backup.create", Label: "Back Up Now"}
}

func (c Create) Run(_ json.RawMessage, obs commands.Observer) {
	info, err := backup.Take(c.db, backup.Manual, "Manual backup")
	if err != nil {
		obs.Finish(nil, err)
		return
	}
	info.Header.AppKey = ""
	info.Header.PasswordKey = nil
	obs.Finish(info, nil)
}
