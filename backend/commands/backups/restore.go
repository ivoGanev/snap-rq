package backups

import (
	"database/sql"
	"encoding/json"
	"errors"

	"snap-rq/backend/backup"
	"snap-rq/backend/commands"
	"snap-rq/backend/database"
)

// restore.go — get a backup ready to restore on the next start (see
// backup.PrepareRestore); the window then restarts the app. Observable: the
// "before restore" backup needs the database itself.

type Restore struct{ db *sql.DB }

func NewRestore(db *sql.DB) commands.ObservableCommand { return &Restore{db: db} }

func (Restore) Info() commands.Info {
	return commands.Info{FunctionID: "backup.restore", Label: "Restore Backup"}
}

// RestoreResult tells the window what to do next.
type RestoreResult struct {
	// Status is "ready" (restart to finish), "needs_key" (ask for the
	// recovery key or backup password) or "wrong_key" (what was entered
	// doesn't open it).
	Status string        `json:"status"`
	Backup backup.Header `json:"backup"`
}

func (c Restore) Run(input json.RawMessage, obs commands.Observer) {
	var in struct {
		Path        string `json:"path"`
		RecoveryKey string `json:"recovery_key"`
		Password    string `json:"password"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		obs.Finish(nil, err)
		return
	}

	h, err := backup.PrepareRestore(c.db, database.DataDir(), in.Path,
		backup.Unlock{RecoveryKey: in.RecoveryKey, Password: in.Password})
	h.AppKey, h.PasswordKey = "", nil
	switch {
	case errors.Is(err, backup.ErrNeedsKey):
		obs.Finish(RestoreResult{Status: "needs_key", Backup: h}, nil)
	case errors.Is(err, backup.ErrWrongUnlock):
		obs.Finish(RestoreResult{Status: "wrong_key", Backup: h}, nil)
	case err != nil:
		obs.Finish(nil, err)
	default:
		obs.Finish(RestoreResult{Status: "ready", Backup: h}, nil)
	}
}
