package favourites

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// set_appearance.go — set a favourite collection's icon or colour.
// Snapshot saves the previous appearance row.

type SetAppearance struct{ tx *sql.Tx }

func NewSetAppearance(tx *sql.Tx) commands.Command { return &SetAppearance{tx: tx} }

func (SetAppearance) Info() commands.Info {
	return commands.Info{FunctionID: "favourite.set_appearance", Label: "Set Favourite Appearance"}
}

func (c SetAppearance) Snapshot(input json.RawMessage) (any, error) {
	var in struct {
		ID    int64  `json:"favourite_collection_id"`
		Type  string `json:"appearance_type"`
		Value string `json:"appearance_value"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	oldType, oldValue := "", ""
	_ = c.tx.QueryRow(`
		SELECT COALESCE(appearance_type, ''), COALESCE(appearance_value, '')
		FROM favourite_appearances WHERE favourite_collection_id = ?`, in.ID,
	).Scan(&oldType, &oldValue)
	return map[string]any{
		"favourite_collection_id": in.ID, "appearance_type": oldType, "appearance_value": oldValue,
	}, nil
}

func (c SetAppearance) Run(input json.RawMessage) (any, error) {
	var in struct {
		ID    int64  `json:"favourite_collection_id"`
		Type  string `json:"appearance_type"`
		Value string `json:"appearance_value"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if in.ID == 0 {
		return nil, fmt.Errorf("favourite collection id is required")
	}
	if in.Type != "icon" && in.Type != "color" {
		return nil, fmt.Errorf("appearance_type must be 'icon' or 'color'")
	}
	if _, err := c.tx.Exec(`
		INSERT INTO favourite_appearances (favourite_collection_id, appearance_type, appearance_value)
		VALUES (?, ?, ?)
		ON CONFLICT(favourite_collection_id)
		DO UPDATE SET appearance_type = excluded.appearance_type, appearance_value = excluded.appearance_value`,
		in.ID, in.Type, in.Value,
	); err != nil {
		return nil, fmt.Errorf("updating favourite appearance: %w", err)
	}
	return models.FavouriteAppearance{
		FavouriteCollectionID: in.ID, AppearanceType: in.Type, AppearanceValue: in.Value,
	}, nil
}

func (c SetAppearance) Undo(snapshot, _ json.RawMessage) error {
	var old struct {
		FavouriteCollectionID int64  `json:"favourite_collection_id"`
		AppearanceType        string `json:"appearance_type"`
		AppearanceValue       string `json:"appearance_value"`
	}
	if err := json.Unmarshal(snapshot, &old); err != nil {
		return err
	}
	if _, err := c.tx.Exec(`
		INSERT INTO favourite_appearances (favourite_collection_id, appearance_type, appearance_value)
		VALUES (?, ?, ?)
		ON CONFLICT(favourite_collection_id)
		DO UPDATE SET appearance_type = excluded.appearance_type, appearance_value = excluded.appearance_value`,
		old.FavouriteCollectionID, old.AppearanceType, old.AppearanceValue,
	); err != nil {
		return err
	}
	return nil
}

func (SetAppearance) Affected(input, _ json.RawMessage) []commands.Affected {
	return []commands.Affected{{Component: "favourite", IDs: commands.IDsOf(input, "favourite_collection_id")}}
}
