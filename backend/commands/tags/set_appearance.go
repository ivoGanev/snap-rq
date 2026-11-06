package tags

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

//  — set a tag's icon or colour. Snapshot saves the
// previous appearance.

type SetAppearance struct{ tx *sql.Tx }

func NewSetAppearance(tx *sql.Tx) commands.Command { return &SetAppearance{tx: tx} }

func (SetAppearance) Info() commands.Info {
	return commands.Info{FunctionID: "tag.set_appearance", Label: "Set Tag Appearance"}
}

func (c SetAppearance) Snapshot(input json.RawMessage) (any, error) {
	var in struct {
		TagID int64  `json:"tag_id"`
		Type  string `json:"appearance_type"`
		Value string `json:"appearance_value"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	oldType, oldValue := "", ""
	_ = c.tx.QueryRow(`
		SELECT COALESCE(appearance_type, ''), COALESCE(appearance_value, '')
		FROM tag_appearances WHERE tag_id = ?`, in.TagID,
	).Scan(&oldType, &oldValue)
	return map[string]any{
		"tag_id": in.TagID, "appearance_type": oldType, "appearance_value": oldValue,
	}, nil
}

func (c SetAppearance) Run(input json.RawMessage) (any, error) {
	var in struct {
		TagID int64  `json:"tag_id"`
		Type  string `json:"appearance_type"`
		Value string `json:"appearance_value"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if in.TagID == 0 {
		return nil, fmt.Errorf("tag id is required")
	}
	if in.Type != "icon" && in.Type != "color" {
		return nil, fmt.Errorf("appearance_type must be 'icon' or 'color'")
	}
	if _, err := c.tx.Exec(`
		INSERT INTO tag_appearances (tag_id, appearance_type, appearance_value)
		VALUES (?, ?, ?)
		ON CONFLICT(tag_id)
		DO UPDATE SET appearance_type = excluded.appearance_type, appearance_value = excluded.appearance_value`,
		in.TagID, in.Type, in.Value,
	); err != nil {
		return nil, fmt.Errorf("updating tag appearance: %w", err)
	}
	return models.TagAppearance{
		TagID: in.TagID, AppearanceType: in.Type, AppearanceValue: in.Value,
	}, nil
}

func (c SetAppearance) Undo(snapshot, _ json.RawMessage) error {
	var old struct {
		TagID           int64  `json:"tag_id"`
		AppearanceType  string `json:"appearance_type"`
		AppearanceValue string `json:"appearance_value"`
	}
	if err := json.Unmarshal(snapshot, &old); err != nil {
		return err
	}
	if _, err := c.tx.Exec(`
		INSERT INTO tag_appearances (tag_id, appearance_type, appearance_value)
		VALUES (?, ?, ?)
		ON CONFLICT(tag_id)
		DO UPDATE SET appearance_type = excluded.appearance_type, appearance_value = excluded.appearance_value`,
		old.TagID, old.AppearanceType, old.AppearanceValue,
	); err != nil {
		return err
	}
	return nil
}

func (SetAppearance) Changes(input, _ json.RawMessage) []commands.RowChange {
	return []commands.RowChange{{Component: "tag", IDs: commands.IDsOf(input, "tag_id")}}
}
