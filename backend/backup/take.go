package backup

import (
	"database/sql"
	"os"
	"time"

	"snap-rq/backend/secrets"
)

// take.go — making a backup with the user's settings, and keeping the
// folder tidy.

// AppVersion is recorded in every backup; main sets it.
var AppVersion = "0.0.0-dev"

// Prepare is Begin with the user's folder and password: the part to run
// before a risky change.
func Prepare(db *sql.DB, kind Kind, reason string) (*Pending, error) {
	s, err := LoadSettings(db)
	if err != nil {
		return nil, err
	}
	password := ""
	if s.PasswordSealed != "" {
		// A locked key can't open the password; back up without it rather
		// than not at all (the app key still seals the backup).
		password, _ = secrets.Default().Decrypt(db, s.PasswordSealed)
	}
	return Begin(db, Options{
		Dir: s.ResolvedFolder(), Kind: kind, Reason: reason, AppVersion: AppVersion, Password: password,
	})
}

// Complete finishes a prepared backup and then applies retention.
func Complete(db *sql.DB, p *Pending) (Info, error) {
	info, err := p.Finish()
	if err != nil {
		return Info{}, err
	}
	if s, err := LoadSettings(db); err == nil {
		Prune(s.ResolvedFolder(), s.KeepAutomatic, time.Duration(s.KeepDays)*24*time.Hour)
	}
	return info, nil
}

// Take makes a whole backup with the user's settings.
func Take(db *sql.DB, kind Kind, reason string) (Info, error) {
	p, err := Prepare(db, kind, reason)
	if err != nil {
		return Info{}, err
	}
	return Complete(db, p)
}

// Prune deletes automatic backups beyond the newest keep, and those older
// than maxAge. Manual backups are never deleted.
func Prune(dir string, keep int, maxAge time.Duration) {
	list, err := List(dir)
	if err != nil {
		return
	}
	kept := 0
	for _, b := range list { // newest first
		if b.Header.Kind != Automatic {
			continue
		}
		if kept >= keep || time.Since(b.Header.CreatedAt) > maxAge {
			os.Remove(b.Path)
			continue
		}
		kept++
	}
}
