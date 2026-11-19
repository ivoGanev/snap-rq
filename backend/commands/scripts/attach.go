package scripts

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// attach.go — attach a script to a request, creating the script (with the
// default appearance) when the project has none with that name. With
// request_id 0 it only creates the script. Snapshot saves whether the script
// and the link existed; undo removes exactly what was created.

type Attach struct{ tx *sql.Tx }

func NewAttach(tx *sql.Tx) commands.Command { return &Attach{tx: tx} }

func (Attach) Info() commands.Info {
	return commands.Info{FunctionID: "script.attach", Label: "Attach Script"}
}

type attachInput struct {
	RequestID  int64  `json:"request_id"`
	ProjectID  int64  `json:"project_id"`
	ScriptName string `json:"script_name"`
}

func (c Attach) Snapshot(input json.RawMessage) (any, error) {
	var in attachInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.ScriptName)

	var scriptID int64
	scriptExists := false
	if err := c.tx.QueryRow(
		`SELECT id FROM scripts WHERE project_id = ? AND name = ?`, in.ProjectID, name,
	).Scan(&scriptID); err == nil {
		scriptExists = true
	}

	linkExists := false
	if scriptExists {
		var one int
		if err := c.tx.QueryRow(
			`SELECT 1 FROM request_scripts WHERE request_id = ? AND script_id = ?`, in.RequestID, scriptID,
		).Scan(&one); err == nil {
			linkExists = true
		}
	}
	return map[string]any{
		"request_id": in.RequestID, "script_existed": scriptExists, "link_existed": linkExists,
	}, nil
}

func (c Attach) Run(input json.RawMessage) (any, error) {
	var in attachInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.ScriptName)
	if name == "" {
		return nil, fmt.Errorf("script name is required")
	}

	var scriptID int64
	err := c.tx.QueryRow(
		`SELECT id FROM scripts WHERE project_id = ? AND name = ?`, in.ProjectID, name,
	).Scan(&scriptID)
	if err == sql.ErrNoRows {
		result, err := c.tx.Exec(
			`INSERT INTO scripts (project_id, name) VALUES (?, ?)`, in.ProjectID, name)
		if err != nil {
			return nil, fmt.Errorf("creating script: %w", err)
		}
		scriptID, _ = result.LastInsertId()
		appearance := models.DefaultScriptAppearance()
		if _, err := c.tx.Exec(
			`INSERT INTO script_appearances (script_id, appearance_type, appearance_value) VALUES (?, ?, ?)`,
			scriptID, appearance.AppearanceType, appearance.AppearanceValue,
		); err != nil {
			return nil, fmt.Errorf("creating script appearance: %w", err)
		}
	} else if err != nil {
		return nil, err
	}

	if in.RequestID != 0 {
		if _, err := c.tx.Exec(
			`INSERT OR IGNORE INTO request_scripts (request_id, script_id) VALUES (?, ?)`,
			in.RequestID, scriptID); err != nil {
			return nil, fmt.Errorf("attaching script: %w", err)
		}
	}
	return models.Script{ID: scriptID, ProjectID: in.ProjectID, Name: name}, nil
}

func (c Attach) Undo(snapshot, result json.RawMessage) error {
	var old struct {
		RequestID     int64 `json:"request_id"`
		ScriptExisted bool  `json:"script_existed"`
		LinkExisted   bool  `json:"link_existed"`
	}
	if err := json.Unmarshal(snapshot, &old); err != nil {
		return err
	}
	var created models.Script
	if err := json.Unmarshal(result, &created); err != nil {
		return err
	}
	// a newly created script takes its links and appearance with it
	if !old.ScriptExisted {
		_, err := c.tx.Exec(`DELETE FROM scripts WHERE id = ?`, created.ID)
		return err
	}
	if !old.LinkExisted {
		_, err := c.tx.Exec(
			`DELETE FROM request_scripts WHERE script_id = ? AND request_id = ?`, created.ID, old.RequestID)
		return err
	}
	return nil
}

func (Attach) Changes(_, result json.RawMessage) []commands.RowChange {
	return []commands.RowChange{{Component: "script", IDs: commands.IDsOf(result, "id")}}
}
