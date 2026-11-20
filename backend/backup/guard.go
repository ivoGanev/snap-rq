package backup

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"snap-rq/backend/commands"
	"snap-rq/backend/secrets"
)

// guard.go — the backup taken before a risky command (see
// commands.BackupGuarded and Runner.SetBackupGuard).

// Throttle skips a backup when another was made this recently: deleting five
// collections in a row costs one pause, not five (undo covers the steps in
// between). A variable so tests can turn it off.
var Throttle = 2 * time.Minute

var (
	lastMu    sync.Mutex
	lastTaken time.Time
)

func markTaken() {
	lastMu.Lock()
	lastTaken = time.Now()
	lastMu.Unlock()
}

func takenRecently() bool {
	lastMu.Lock()
	defer lastMu.Unlock()
	return time.Since(lastTaken) < Throttle
}

// Guard returns the runner's backup guard. The snapshot is taken before the
// command (about a third of a second for 50 MB); compressing, encrypting and
// writing the file happen in the background afterwards, and saved is called
// with the result (for the "Backup saved" toast).
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
		if takenRecently() {
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
			markTaken()
			go func() {
				info, err := Complete(db, pending)
				if err == nil && saved != nil {
					saved(info)
				}
			}()
		}, nil
	}
}
