package favourites

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// list.go — a project's favourite collections with their appearances.

type List struct{ tx *sql.Tx }

func NewList(tx *sql.Tx) commands.Command { return &List{tx: tx} }

func (List) Info() commands.Info {
	return commands.Info{FunctionID: "favourite.list", Label: "List Favourites", ReadOnly: true}
}

func (c List) Run(input json.RawMessage) (any, error) {
	var in struct {
		ProjectID int64 `json:"project_id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	rows, err := c.tx.Query(`
		SELECT fc.id, fc.project_id, fc.name, fc.created_at,
		       COALESCE(fa.appearance_type, 'icon'), COALESCE(fa.appearance_value, 'default')
		FROM favourite_collections fc
		LEFT JOIN favourite_appearances fa ON fa.favourite_collection_id = fc.id
		WHERE fc.project_id = ?
		ORDER BY fc.created_at DESC`, in.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("listing favourite collections: %w", err)
	}
	defer rows.Close()

	var collections []models.FavouriteCollection
	for rows.Next() {
		var col models.FavouriteCollection
		if err := rows.Scan(&col.ID, &col.ProjectID, &col.Name, &col.CreatedAt,
			&col.Appearance.AppearanceType, &col.Appearance.AppearanceValue); err != nil {
			return nil, fmt.Errorf("reading a favourite collection: %w", err)
		}
		col.Appearance.FavouriteCollectionID = col.ID
		collections = append(collections, col)
	}
	return collections, rows.Err()
}
