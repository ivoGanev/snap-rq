package scripts

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"snap-rq/backend/commands"
)

// detach.go — detach a script from a request; the script itself stays.
// Snapshot saves the link row; undo re-inserts it.

type Detach struct{ tx *sql.Tx }

func NewDetach(tx *sql.Tx) commands.Command { return &Detach{tx: tx} }

func (Detach) Info() commands.Info {
	return commands.Info{FunctionID: "script.detach", Label: "Detach Script"}
}

type savedLink struct {
	ID            int64   `json:"id"`
	RequestID     int64   `json:"request_id"`
	ScriptID      int64   `json:"script_id"`
	UserSortOrder int     `json:"user_sort_order"`
	UserColor     *string `json:"user_color"`
}

type detachInput struct {
	RequestID  int64  `json:"request_id"`
	ScriptName string `json:"script_name"`
}

// linkOf finds the request's link to the named script of the request's own
// project (script names are only unique within a project).
func (c Detach) linkOf(in detachInput) (savedLink, error) {
	var link savedLink
	err := c.tx.QueryRow(`
		SELECT rs.id, rs.request_id, rs.script_id, rs.user_sort_order, rs.user_color
		FROM request_scripts rs
		JOIN scripts s ON s.id = rs.script_id
		JOIN http_requests hr ON hr.id = rs.request_id
		JOIN collections c ON c.id = hr.collection_id
		WHERE rs.request_id = ? AND s.name = ? AND s.project_id = c.project_id`,
		in.RequestID, in.ScriptName,
	).Scan(&link.ID, &link.RequestID, &link.ScriptID, &link.UserSortOrder, &link.UserColor)
	if err != nil {
		return savedLink{}, fmt.Errorf("script not attached to request: %w", err)
	}
	return link, nil
}

func (c Detach) Snapshot(input json.RawMessage) (any, error) {
	var in detachInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	return c.linkOf(in)
}

func (c Detach) Run(input json.RawMessage) (any, error) {
	var in detachInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	link, err := c.linkOf(in)
	if err != nil {
		return nil, err
	}
	_, err = c.tx.Exec(`DELETE FROM request_scripts WHERE id = ?`, link.ID)
	return nil, err
}

func (c Detach) Undo(snapshot, _ json.RawMessage) error {
	var link savedLink
	if err := json.Unmarshal(snapshot, &link); err != nil {
		return err
	}
	_, err := c.tx.Exec(`
		INSERT INTO request_scripts (id, request_id, script_id, user_sort_order, user_color)
		VALUES (?, ?, ?, ?, ?)`,
		link.ID, link.RequestID, link.ScriptID, link.UserSortOrder, link.UserColor)
	return err
}

func (Detach) Changes(_, _ json.RawMessage) []commands.RowChange {
	return []commands.RowChange{{Component: "script"}}
}
