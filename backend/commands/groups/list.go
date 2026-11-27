package groups

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// list.go — every request group of one project.

type List struct{ tx *sql.Tx }

func NewList(tx *sql.Tx) commands.Command { return &List{tx: tx} }

func (List) Info() commands.Info {
	return commands.Info{FunctionID: "group.list", Label: "List Groups", ReadOnly: true}
}

func (c List) Run(input json.RawMessage) (any, error) {
	var in struct {
		ProjectID int64 `json:"project_id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	rows, err := c.tx.Query(`
		SELECT g.id, g.collection_id, g.name, g.color
		FROM request_groups g JOIN collections c ON c.id = g.collection_id
		WHERE c.project_id = ? ORDER BY g.name`, in.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("listing groups: %w", err)
	}
	defer rows.Close()
	groups := []models.RequestGroup{}
	for rows.Next() {
		var g models.RequestGroup
		if err := rows.Scan(&g.ID, &g.CollectionID, &g.Name, &g.Color); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}
