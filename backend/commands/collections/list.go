package collections

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// list.go — a project's collections with their appearances.

type List struct{ tx *sql.Tx }

func NewList(tx *sql.Tx) commands.Command { return &List{tx: tx} }

func (List) Info() commands.Info {
	return commands.Info{FunctionID: "collection.list", Label: "List Collections", ReadOnly: true}
}

func (c List) Run(input json.RawMessage) (any, error) {
	var in struct {
		ProjectID int64 `json:"project_id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	rows, err := c.tx.Query(`
		SELECT col.id, col.project_id, col.name,
		       COALESCE(ca.appearance_type, 'icon'), COALESCE(ca.appearance_value, 'default')
		FROM collections col
		LEFT JOIN collection_appearances ca ON ca.collection_id = col.id
		WHERE col.project_id = ?
		ORDER BY col.name`, in.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("listing collections: %w", err)
	}
	defer rows.Close()

	var collections []models.Collection
	for rows.Next() {
		var col models.Collection
		if err := rows.Scan(&col.ID, &col.ProjectID, &col.Name,
			&col.Appearance.AppearanceType, &col.Appearance.AppearanceValue); err != nil {
			return nil, fmt.Errorf("reading a collection: %w", err)
		}
		col.Appearance.CollectionID = col.ID
		collections = append(collections, col)
	}
	return collections, rows.Err()
}
