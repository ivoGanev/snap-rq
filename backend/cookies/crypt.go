package cookies

import (
	"database/sql"
	"errors"

	"snap-rq/backend/secrets"
)

// crypt.go — cookie values are always stored encrypted with the secrets
// vault's key (session cookies are credentials). Only the value: name,
// domain, path and expiry stay readable, so matching works as before.
//
// A value that can't be decrypted (an old plain value, or a key from
// elsewhere) is unusable and gets dropped: cookies are disposable, the server
// issues new ones on the next login.

// Querier is what the cookie functions need from the database; *sql.DB and
// *sql.Tx satisfy it.
type Querier interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
	Exec(query string, args ...any) (sql.Result, error)
}

// Seal encrypts a cookie value for storing.
func Seal(q Querier, value string) (string, error) {
	return secrets.Default().Encrypt(q, value)
}

// Open decrypts a stored cookie value.
func Open(q Querier, stored string) (string, error) {
	return secrets.Default().Decrypt(q, stored)
}

// EnsureUsable makes the key usable for cookies. When it is locked (missing
// or from another database) and no secret variable has a value, nothing worth
// recovering depends on it: every cookie is cleared and the key check
// forgotten, so the next value is encrypted with a working key. With secret
// variables it stays locked (secrets.ErrLocked) until the user unlocks or
// starts over in the variables editor.
func EnsureUsable(q Querier) error {
	vault := secrets.Default()
	state, err := vault.Status(q)
	if err != nil || state != secrets.StateLocked {
		return err
	}
	var withValues int
	if err := q.QueryRow(
		`SELECT COUNT(*) FROM environment_variables WHERE secret = 1 AND value != ''`,
	).Scan(&withValues); err != nil {
		return err
	}
	if withValues > 0 {
		return secrets.ErrLocked
	}
	if _, err := q.Exec(`DELETE FROM cookies`); err != nil {
		return err
	}
	return vault.Forget(q)
}

// isLocked reports a locked key (keep the cookies, they may come back) as
// opposed to a value that will never decrypt (drop it).
func isLocked(err error) bool { return errors.Is(err, secrets.ErrLocked) }
