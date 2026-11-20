package backup

import (
	"database/sql"
	"errors"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/secrets"
)

// guard.go — the backup taken before a risky command (see
// commands.BackupGuarded and Runner.SetBackupGuard).

// Guard returns the runner's backup guard. Every risky command gets its own
// backup, no matter how recent the last one is: retention (KeepRisky) limits
// how many are kept. The snapshot is taken before the command (about a third
// of a second for 50 MB); compressing, encrypting and writing the file happen
// in the background afterwards, and saved is called with the result (for the
// "Backup saved" toast).
func Guard(db *sql.DB, saved func(Info)) commands.BackupGuard {
	return func(need commands.BackupNeed) (func(bool), error) {
		s, err := LoadSettings(db)
		if err != nil {
			return nil, err
		}
		trigger := Trigger(need.Trigger)
		if !s.Wants(trigger) {
			return nil, nil
		}
		if trigger == TriggerDeleteRequests && need.Items < s.RequestDeleteThreshold {
			return nil, nil
		}
		pending, err := Prepare(db, Automatic, need.Reason)
		if errors.Is(err, secrets.ErrLocked) {
			// The key is locked and there is no backup password: a backup
			// couldn't be opened, so don't block the user (this is the
			// "Start over" case).
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("nothing was changed: the backup %s failed: %w", need.Reason, err)
		}
		return func(committed bool) {
			if !committed {
				pending.Discard()
				return
			}
			go func() {
				info, err := Complete(db, pending)
				if err == nil && saved != nil {
					saved(info)
				}
			}()
		}, nil
	}
}
