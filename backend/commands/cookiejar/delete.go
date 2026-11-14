package cookiejar

import (
	"database/sql"
	"encoding/json"
	"errors"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// delete.go — remove one cookie. Snapshot saves the row; undo re-inserts it
// under its original id.

type Delete struct{ tx *sql.Tx }

func NewDelete(tx *sql.Tx) commands.Command { return &Delete{tx: tx} }

func (Delete) Info() commands.Info {
	return commands.Info{FunctionID: "cookie.delete", Label: "Delete Cookie"}
}

func (c Delete) Snapshot(input json.RawMessage) (any, error) {
	var in struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	return loadCookie(c.tx, in.ID)
}

func (c Delete) Run(input json.RawMessage) (any, error) {
	var in struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if in.ID == 0 {
		return nil, errors.New("cookie id is required")
	}
	res, err := c.tx.Exec(`DELETE FROM cookies WHERE id = ?`, in.ID)
	if err != nil {
		return nil, err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return nil, errors.New("cookie not found")
	}
	return nil, nil
}

func (c Delete) Undo(snapshot, _ json.RawMessage) error {
	var saved models.Cookie
	if err := json.Unmarshal(snapshot, &saved); err != nil {
		return err
	}
	_, err := insertCookie(c.tx, saved)
	return err
}

func (Delete) Changes(input, _ json.RawMessage) []commands.RowChange {
	return []commands.RowChange{{Component: "cookie", IDs: commands.IDsOf(input, "id")}}
}
