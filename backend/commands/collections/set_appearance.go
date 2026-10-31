package collections

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// set_appearance.go — set a collection's icon or colour. Snapshot saves
// the previous appearance.

type SetAppearance struct{ tx *sql.Tx }

func NewSetAppearance(tx *sql.Tx) commands.Command { return &SetAppearance{tx: tx} }

func (SetAppearance) Info() commands.Info {
	return commands.Info{FunctionID: "collection.set_appearance", Label: "Set Collection Appearance"}
}

func (c SetAppearance) Snapshot(input json.RawMessage) (any, error) {
	var in struct {
		CollectionID int64  `json:"collection_id"`
		Type         string `json:"appearance_type"`
		Value        string `json:"appearance_value"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	oldType, oldValue := "", ""
	_ = c.tx.QueryRow(`
		SELECT COALESCE(appearance_type, ''), COALESCE(appearance_value, '')
		FROM collection_appearances WHERE collection_id = ?`, in.CollectionID,
	).Scan(&oldType, &oldValue)
	return map[string]any{
		"collection_id": in.CollectionID, "appearance_type": oldType, "appearance_value": oldValue,
	}, nil
}

func (c SetAppearance) Run(input json.RawMessage) (any, error) {
	var in struct {
		CollectionID int64  `json:"collection_id"`
		Type         string `json:"appearance_type"`
		Value        string `json:"appearance_value"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if in.CollectionID == 0 {
		return nil, fmt.Errorf("collection id is required")
	}
	if in.Type != "icon" && in.Type != "color" {
		return nil, fmt.Errorf("appearance_type must be 'icon' or 'color'")
	}
	if _, err := c.tx.Exec(`
		INSERT INTO collection_appearances (collection_id, appearance_type, appearance_value)
		VALUES (?, ?, ?)
		ON CONFLICT(collection_id)
		DO UPDATE SET appearance_type = excluded.appearance_type, appearance_value = excluded.appearance_value`,
		in.CollectionID, in.Type, in.Value,
	); err != nil {
		return nil, fmt.Errorf("updating collection appearance: %w", err)
	}
	return models.CollectionAppearance{
		CollectionID: in.CollectionID, AppearanceType: in.Type, AppearanceValue: in.Value,
	}, nil
}

func (c SetAppearance) Undo(snapshot, _ json.RawMessage) error {
	var old struct {
		CollectionID    int64  `json:"collection_id"`
		AppearanceType  string `json:"appearance_type"`
		AppearanceValue string `json:"appearance_value"`
	}
	if err := json.Unmarshal(snapshot, &old); err != nil {
		return err
	}
	if _, err := c.tx.Exec(`
		INSERT INTO collection_appearances (collection_id, appearance_type, appearance_value)
		VALUES (?, ?, ?)
		ON CONFLICT(collection_id)
		DO UPDATE SET appearance_type = excluded.appearance_type, appearance_value = excluded.appearance_value`,
		old.CollectionID, old.AppearanceType, old.AppearanceValue,
	); err != nil {
		return err
	}
	return nil
}
