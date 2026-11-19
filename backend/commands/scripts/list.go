package scripts

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// list.go — a project's scripts, without their content.

type List struct{ tx *sql.Tx }

func NewList(tx *sql.Tx) commands.Command { return &List{tx: tx} }

func (List) Info() commands.Info {
	return commands.Info{FunctionID: "script.list", Label: "List Scripts", ReadOnly: true}
}

func (c List) Run(input json.RawMessage) (any, error) {
	var in struct {
		ProjectID int64 `json:"project_id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	rows, err := c.tx.Query(`
		SELECT t.id, t.project_id, t.name,
		       COALESCE(ta.appearance_type, 'icon'), COALESCE(ta.appearance_value, 'default')
		FROM scripts t
		LEFT JOIN script_appearances ta ON ta.script_id = t.id
		WHERE t.project_id = ?
		ORDER BY t.name`, in.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("listing scripts: %w", err)
	}
	defer rows.Close()

	var scripts []models.Script
	for rows.Next() {
		var script models.Script
		if err := rows.Scan(&script.ID, &script.ProjectID, &script.Name,
			&script.Appearance.AppearanceType, &script.Appearance.AppearanceValue); err != nil {
			return nil, err
		}
		script.Appearance.ScriptID = script.ID
		scripts = append(scripts, script)
	}
	return scripts, rows.Err()
}
