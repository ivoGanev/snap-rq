package cookiejar

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// save.go — create a cookie by hand, or edit a stored one (an input id
// means edit). Undo removes the new cookie or restores the old values.

type Save struct{ tx *sql.Tx }

func NewSave(tx *sql.Tx) commands.Command { return &Save{tx: tx} }

func (Save) Info() commands.Info {
	return commands.Info{FunctionID: "cookie.save", Label: "Save Cookie"}
}

// Snapshot keeps the cookie as it was before an edit; creating has nothing to keep.
func (c Save) Snapshot(input json.RawMessage) (any, error) {
	var in models.Cookie
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if in.ID == 0 {
		return nil, nil
	}
	return loadCookie(c.tx, in.ID)
}

func (c Save) Run(input json.RawMessage) (any, error) {
	var in models.Cookie
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if err := normalize(&in); err != nil {
		return nil, err
	}

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
		return loadCookie(c.tx, id)
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
	return loadCookie(c.tx, in.ID)
}

func (c Save) Undo(snapshot, result json.RawMessage) error {
	if len(snapshot) == 0 || string(snapshot) == "null" {
		var created models.Cookie
		if err := json.Unmarshal(result, &created); err != nil {
			return err
		}
		_, err := c.tx.Exec(`DELETE FROM cookies WHERE id = ?`, created.ID)
		return err
	}
	var old models.Cookie
	if err := json.Unmarshal(snapshot, &old); err != nil {
		return err
	}
	_, err := c.tx.Exec(`UPDATE cookies SET domain = ?, host_only = ?, path = ?, name = ?, value = ?,
		expires_at = ?, secure = ?, http_only = ?, same_site = ? WHERE id = ?`,
		old.Domain, old.HostOnly, old.Path, old.Name, old.Value,
		old.ExpiresAt, old.Secure, old.HTTPOnly, old.SameSite, old.ID)
	return err
}

func (Save) Changes(_, result json.RawMessage) []commands.RowChange {
	return []commands.RowChange{{Component: "cookie", IDs: commands.IDsOf(result, "id")}}
}
