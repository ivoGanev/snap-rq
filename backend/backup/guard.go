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
// how many are kept.
//
// Only freezing the snapshot happens before the command (a millisecond or
// two); if that fails, the command is cancelled. Copying, compressing,
// encrypting and writing happen in the background once the command has
// committed, one backup at a time. saved or failed is called with the
// outcome (the "Backup saved" or "Backup failed" toast): by then the change
// has happened, so a failure can only be reported.
func Guard(db *sql.DB, saved func(Info), failed func(reason string, err error)) commands.BackupGuard {
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
				switch {
				case err != nil && failed != nil:
					failed(need.Reason, err)
				case err == nil && saved != nil:
					saved(info)
				}
			}()
		}, nil
	}
}
