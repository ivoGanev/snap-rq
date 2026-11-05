// Package favourites holds the favourite collection commands, one
// operation per file.
package favourites

import (
	"database/sql"
	"encoding/json"
	"errors"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// create.go — save a favourite collection with its default appearance.
// Undo removes it; cascades take the appearance with it.

type Create struct{ tx *sql.Tx }

func NewCreate(tx *sql.Tx) commands.Command { return &Create{tx: tx} }

func (Create) Info() commands.Info {
	return commands.Info{FunctionID: "favourite.create", Label: "Create Favourite"}
}

// Snapshot: nothing existed before a create.
func (Create) Snapshot(_ json.RawMessage) (any, error) { return nil, nil }

func (c Create) Run(input json.RawMessage) (any, error) {
	var in struct {
		ProjectID int64  `json:"project_id"`
		Name      string `json:"name"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if in.ProjectID == 0 {
		return nil, errors.New("project id is required")
	}

	result, err := c.tx.Exec(
		`INSERT INTO favourite_collections (project_id, name) VALUES (?, ?)`,
		in.ProjectID, in.Name)
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	defaultAppearance := models.DefaultFavouriteAppearance()
	if _, err := c.tx.Exec(
		`INSERT INTO favourite_appearances (favourite_collection_id, appearance_type, appearance_value)
		 VALUES (?, ?, ?)`,
		id, defaultAppearance.AppearanceType, defaultAppearance.AppearanceValue,
	); err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "project_id": in.ProjectID, "name": in.Name}, nil
}

// Undo deletes the collection; cascades remove its appearance and items.
func (c Create) Undo(_, result json.RawMessage) error {
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(result, &created); err != nil {
		return err
	}
	_, err := c.tx.Exec(`DELETE FROM favourite_collections WHERE id = ?`, created.ID)
	return err
}

func (Create) Affected(_, result json.RawMessage) []commands.Affected {
	return []commands.Affected{{Component: "favourite", IDs: commands.IDsOf(result, "id")}}
}
