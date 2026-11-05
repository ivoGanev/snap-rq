// Package collections holds the collection commands, one operation per
// file.
package collections

import (
	"database/sql"
	"encoding/json"
	"errors"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// create.go — save a collection with its default appearance. Undo removes
// it; cascades take the appearance with it.

type Create struct{ tx *sql.Tx }

func NewCreate(tx *sql.Tx) commands.Command { return &Create{tx: tx} }

func (Create) Info() commands.Info {
	return commands.Info{FunctionID: "collection.create", Label: "Create Collection"}
}

func (Create) Snapshot(_ json.RawMessage) (any, error) { return nil, nil }

func (c Create) Run(input json.RawMessage) (any, error) {
	var in models.Collection
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if in.ProjectID == 0 {
		return nil, errors.New("project id is required")
	}

	result, err := c.tx.Exec(
		`INSERT INTO collections (project_id, name) VALUES (?, ?)`, in.ProjectID, in.Name)
	if err != nil {
		return nil, err
	}
	id, _ := result.LastInsertId()
	in.ID = id

	defaultAppearance := models.DefaultCollectionAppearance()
	if _, err := c.tx.Exec(
		`INSERT INTO collection_appearances (collection_id, appearance_type, appearance_value)
		 VALUES (?, ?, ?)`,
		id, defaultAppearance.AppearanceType, defaultAppearance.AppearanceValue,
	); err != nil {
		return nil, err
	}
	in.Appearance = defaultAppearance
	return in, nil
}

func (c Create) Undo(_, result json.RawMessage) error {
	var created models.Collection
	if err := json.Unmarshal(result, &created); err != nil {
		return err
	}
	_, err := c.tx.Exec(`DELETE FROM collections WHERE id = ?`, created.ID)
	return err
}

func (Create) Affected(_, result json.RawMessage) []commands.Affected {
	return []commands.Affected{{Component: "collection", IDs: commands.IDsOf(result, "id")}}
}
