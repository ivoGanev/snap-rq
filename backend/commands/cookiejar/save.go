package cookiejar

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"snap-rq/backend/commands"
	"snap-rq/backend/cookies"
	"snap-rq/backend/models"
)

// save.go — create a cookie by hand, or edit a stored one (an input id
// means edit). The value is stored encrypted (see backend/cookies). Not
// undoable, like variable.update: the undo log keeps a command's input, which
// would hold the typed value in plain text. Deleting a cookie stays undoable.

type Save struct{ tx *sql.Tx }

func NewSave(tx *sql.Tx) commands.Command { return &Save{tx: tx} }

func (Save) Info() commands.Info {
	return commands.Info{FunctionID: "cookie.save", Label: "Save Cookie"}
}

func (c Save) Run(input json.RawMessage) (any, error) {
	var in models.Cookie
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if err := normalize(&in); err != nil {
		return nil, err
	}
	plain := in.Value
	sealed, err := cookies.Seal(c.tx, plain)
	if err != nil {
		return nil, err
	}
	in.Value = sealed

	if in.ID == 0 {
		if in.ProjectID == 0 {
			return nil, errors.New("project id is required")
		}
		in.CreatedAt = time.Now().Unix()
		id, err := insertCookie(c.tx, in)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return nil, errors.New("a cookie with that domain, path and name already exists")
			}
			return nil, err
		}
		return loadWithValue(c.tx, id, plain)
	}

	res, err := c.tx.Exec(`UPDATE cookies SET domain = ?, host_only = ?, path = ?, name = ?, value = ?,
		expires_at = ?, secure = ?, http_only = ?, same_site = ? WHERE id = ?`,
		in.Domain, in.HostOnly, in.Path, in.Name, in.Value,
		in.ExpiresAt, in.Secure, in.HTTPOnly, in.SameSite, in.ID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, errors.New("a cookie with that domain, path and name already exists")
		}
		return nil, err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return nil, errors.New("cookie not found")
	}
	return loadWithValue(c.tx, in.ID, plain)
}

// loadWithValue reads the saved cookie back for the editor, with the plain
// value it was given instead of the stored, encrypted one.
func loadWithValue(tx *sql.Tx, id int64, plain string) (models.Cookie, error) {
	saved, err := loadCookie(tx, id)
	if err != nil {
		return models.Cookie{}, err
	}
	saved.Value = plain
	return saved, nil
}

func (Save) Changes(_, result json.RawMessage) []commands.RowChange {
	return []commands.RowChange{{Component: "cookie", IDs: commands.IDsOf(result, "id")}}
}
