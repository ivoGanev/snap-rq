package tags

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

//  — a project's tags.

type List struct{ tx *sql.Tx }

func NewList(tx *sql.Tx) commands.Command { return &List{tx: tx} }

func (List) Info() commands.Info {
	return commands.Info{FunctionID: "tag.list", Label: "List Tags", ReadOnly: true}
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
		FROM tags t
		LEFT JOIN tag_appearances ta ON ta.tag_id = t.id
		WHERE t.project_id = ?
		ORDER BY t.name`, in.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("listing tags: %w", err)
	}
	defer rows.Close()

	var tags []models.Tag
	for rows.Next() {
		var tag models.Tag
		if err := rows.Scan(&tag.ID, &tag.ProjectID, &tag.Name,
			&tag.Appearance.AppearanceType, &tag.Appearance.AppearanceValue); err != nil {
			return nil, err
		}
		tag.Appearance.TagID = tag.ID
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}
