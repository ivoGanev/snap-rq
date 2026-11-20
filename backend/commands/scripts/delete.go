package scripts

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// delete.go — delete a script with its content, links and appearance.
// Snapshot saves all of them; undo re-inserts everything.

type Delete struct{ tx *sql.Tx }

func NewDelete(tx *sql.Tx) commands.Command { return &Delete{tx: tx} }

func (Delete) Info() commands.Info {
	return commands.Info{FunctionID: "script.delete", Label: "Delete Script"}
}

type savedScript struct {
	Script          models.Script `json:"script"`
	AppearanceType  string        `json:"appearance_type"`
	AppearanceValue string        `json:"appearance_value"`
	Links           []savedLink   `json:"links"`
}

type scriptRef struct {
	ProjectID  int64  `json:"project_id"`
	ScriptName string `json:"script_name"`
}

// snapshotScript reads everything needed to recreate a script.
func snapshotScript(tx *sql.Tx, ref scriptRef) (savedScript, error) {
	saved := savedScript{}
	err := tx.QueryRow(`
		SELECT s.id, s.project_id, s.name, s.content,
		       COALESCE(sa.appearance_type, ''), COALESCE(sa.appearance_value, '')
		FROM scripts s
		LEFT JOIN script_appearances sa ON sa.script_id = s.id
		WHERE s.project_id = ? AND s.name = ?`, ref.ProjectID, ref.ScriptName,
	).Scan(&saved.Script.ID, &saved.Script.ProjectID, &saved.Script.Name, &saved.Script.Content,
		&saved.AppearanceType, &saved.AppearanceValue)
	if err != nil {
		return savedScript{}, fmt.Errorf("script not found: %w", err)
	}

	rows, err := tx.Query(`
		SELECT id, request_id, script_id, user_sort_order, user_color
		FROM request_scripts WHERE script_id = ? ORDER BY id`, saved.Script.ID)
	if err != nil {
		return savedScript{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var link savedLink
		if err := rows.Scan(&link.ID, &link.RequestID, &link.ScriptID,
			&link.UserSortOrder, &link.UserColor); err != nil {
			return savedScript{}, err
		}
		saved.Links = append(saved.Links, link)
	}
	return saved, rows.Err()
}

func (c Delete) Snapshot(input json.RawMessage) (any, error) {
	var ref scriptRef
	if err := json.Unmarshal(input, &ref); err != nil {
		return nil, err
	}
	return snapshotScript(c.tx, ref)
}

func (c Delete) Run(input json.RawMessage) (any, error) {
	var ref scriptRef
	if err := json.Unmarshal(input, &ref); err != nil {
		return nil, err
	}
	_, err := c.tx.Exec(
		`DELETE FROM scripts WHERE project_id = ? AND name = ?`, ref.ProjectID, ref.ScriptName)
	return nil, err
}

func (c Delete) Undo(snapshot, _ json.RawMessage) error {
	var saved savedScript
	if err := json.Unmarshal(snapshot, &saved); err != nil {
		return err
	}
	if _, err := c.tx.Exec(
		`INSERT INTO scripts (id, project_id, name, content) VALUES (?, ?, ?, ?)`,
		saved.Script.ID, saved.Script.ProjectID, saved.Script.Name, saved.Script.Content,
	); err != nil {
		return err
	}
	if saved.AppearanceType != "" {
		if _, err := c.tx.Exec(
			`INSERT INTO script_appearances (script_id, appearance_type, appearance_value) VALUES (?, ?, ?)`,
			saved.Script.ID, saved.AppearanceType, saved.AppearanceValue,
		); err != nil {
			return err
		}
	}
	for _, link := range saved.Links {
		if _, err := c.tx.Exec(`
			INSERT INTO request_scripts (id, request_id, script_id, user_sort_order, user_color)
			VALUES (?, ?, ?, ?, ?)`,
			link.ID, link.RequestID, link.ScriptID, link.UserSortOrder, link.UserColor,
		); err != nil {
			return err
		}
	}
	return nil
}

func (Delete) Changes(_, _ json.RawMessage) []commands.RowChange {
	return []commands.RowChange{{Component: "script"}}
}
