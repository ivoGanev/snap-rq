package scripts

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// set_appearance.go — set a script's icon or colour. Snapshot saves the
// previous appearance.

type SetAppearance struct{ tx *sql.Tx }

func NewSetAppearance(tx *sql.Tx) commands.Command { return &SetAppearance{tx: tx} }

func (SetAppearance) Info() commands.Info {
	return commands.Info{FunctionID: "script.set_appearance", Label: "Set Script Appearance"}
}

func (c SetAppearance) Snapshot(input json.RawMessage) (any, error) {
	var in struct {
		ScriptID int64  `json:"script_id"`
		Type     string `json:"appearance_type"`
		Value    string `json:"appearance_value"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	oldType, oldValue := "", ""
	_ = c.tx.QueryRow(`
		SELECT COALESCE(appearance_type, ''), COALESCE(appearance_value, '')
		FROM script_appearances WHERE script_id = ?`, in.ScriptID,
	).Scan(&oldType, &oldValue)
	return map[string]any{
		"script_id": in.ScriptID, "appearance_type": oldType, "appearance_value": oldValue,
	}, nil
}

func (c SetAppearance) Run(input json.RawMessage) (any, error) {
	var in struct {
		ScriptID int64  `json:"script_id"`
		Type     string `json:"appearance_type"`
		Value    string `json:"appearance_value"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if in.ScriptID == 0 {
		return nil, fmt.Errorf("script id is required")
	}
	if in.Type != "icon" && in.Type != "color" {
		return nil, fmt.Errorf("appearance_type must be 'icon' or 'color'")
	}
	if _, err := c.tx.Exec(`
		INSERT INTO script_appearances (script_id, appearance_type, appearance_value)
		VALUES (?, ?, ?)
		ON CONFLICT(script_id)
		DO UPDATE SET appearance_type = excluded.appearance_type, appearance_value = excluded.appearance_value`,
		in.ScriptID, in.Type, in.Value,
	); err != nil {
		return nil, fmt.Errorf("updating script appearance: %w", err)
	}
	return models.ScriptAppearance{
		ScriptID: in.ScriptID, AppearanceType: in.Type, AppearanceValue: in.Value,
	}, nil
}

func (c SetAppearance) Undo(snapshot, _ json.RawMessage) error {
	var old struct {
		ScriptID        int64  `json:"script_id"`
		AppearanceType  string `json:"appearance_type"`
		AppearanceValue string `json:"appearance_value"`
	}
	if err := json.Unmarshal(snapshot, &old); err != nil {
		return err
	}
	if _, err := c.tx.Exec(`
		INSERT INTO script_appearances (script_id, appearance_type, appearance_value)
		VALUES (?, ?, ?)
		ON CONFLICT(script_id)
		DO UPDATE SET appearance_type = excluded.appearance_type, appearance_value = excluded.appearance_value`,
		old.ScriptID, old.AppearanceType, old.AppearanceValue,
	); err != nil {
		return err
	}
	return nil
}

func (SetAppearance) Changes(input, _ json.RawMessage) []commands.RowChange {
	return []commands.RowChange{{Component: "script", IDs: commands.IDsOf(input, "script_id")}}
}
