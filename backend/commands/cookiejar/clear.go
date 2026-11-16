package cookiejar

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// clear.go — remove every cookie of a project, or only those of one domain.
// Snapshot saves the rows; undo puts them back under their original ids.

type Clear struct{ tx *sql.Tx }

func NewClear(tx *sql.Tx) commands.Command { return &Clear{tx: tx} }

func (Clear) Info() commands.Info {
	return commands.Info{FunctionID: "cookie.clear", Label: "Clear Cookies"}
}

type clearInput struct {
	ProjectID int64  `json:"project_id"`
	Domain    string `json:"domain"` // empty means every domain
}

func (in clearInput) where() (string, []any) {
	if d := strings.ToLower(strings.TrimSpace(in.Domain)); d != "" {
		return `project_id = ? AND domain = ?`, []any{in.ProjectID, d}
	}
	return `project_id = ?`, []any{in.ProjectID}
}

func (c Clear) Snapshot(input json.RawMessage) (any, error) {
	var in clearInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	where, args := in.where()
	rows, err := c.tx.Query(`SELECT `+cookieColumns+` FROM cookies WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	saved := []models.Cookie{}
	for rows.Next() {
		cookie, err := scanCookie(rows)
		if err != nil {
			return nil, err
		}
		saved = append(saved, cookie)
	}
	return saved, rows.Err()
}

func (c Clear) Run(input json.RawMessage) (any, error) {
	var in clearInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if in.ProjectID == 0 {
		return nil, errors.New("project id is required")
	}
	// Report which rows went, for the UI refresh.
	where, args := in.where()
	rows, err := c.tx.Query(`SELECT id FROM cookies WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if _, err := c.tx.Exec(`DELETE FROM cookies WHERE `+where, args...); err != nil {
		return nil, err
	}
	return struct {
		IDs []int64 `json:"ids"`
	}{ids}, nil
}

func (c Clear) Undo(snapshot, _ json.RawMessage) error {
	var saved []models.Cookie
	if err := json.Unmarshal(snapshot, &saved); err != nil {
		return err
	}
	for _, cookie := range saved {
		if _, err := insertCookie(c.tx, cookie); err != nil {
			return err
		}
	}
	return nil
}

func (Clear) Changes(_, result json.RawMessage) []commands.RowChange {
	return []commands.RowChange{{Component: "cookie", IDs: commands.IDsOf(result, "ids")}}
}
