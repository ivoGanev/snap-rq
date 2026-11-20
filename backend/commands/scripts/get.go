package scripts

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// get.go — one script with its content, for the script editor.

type Get struct{ tx *sql.Tx }

func NewGet(tx *sql.Tx) commands.Command { return &Get{tx: tx} }

func (Get) Info() commands.Info {
	return commands.Info{FunctionID: "script.get", Label: "Get Script", ReadOnly: true}
}

func (c Get) Run(input json.RawMessage) (any, error) {
	var in struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	var script models.Script
	err := c.tx.QueryRow(`
		SELECT s.id, s.project_id, s.name, s.content,
		       COALESCE(sa.appearance_type, 'icon'), COALESCE(sa.appearance_value, 'default')
		FROM scripts s
		LEFT JOIN script_appearances sa ON sa.script_id = s.id
		WHERE s.id = ?`, in.ID,
	).Scan(&script.ID, &script.ProjectID, &script.Name, &script.Content,
		&script.Appearance.AppearanceType, &script.Appearance.AppearanceValue)
	if err != nil {
		return nil, fmt.Errorf("script not found: %w", err)
	}
	script.Appearance.ScriptID = script.ID
	return script, nil
}
