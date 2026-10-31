package tags

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"snap-rq/backend/commands"
)

//  — persist a custom order for one tag's requests as one bulk
// statement. Snapshot saves the old order; undo restores it with one
// symmetric bulk statement.

type Reorder struct{ tx *sql.Tx }

func NewReorder(tx *sql.Tx) commands.Command { return &Reorder{tx: tx} }

func (Reorder) Info() commands.Info {
	return commands.Info{FunctionID: "tag.reorder", Label: "Reorder Tag"}
}

type oldOrder struct {
	ID            int64 `json:"id"`
	UserSortOrder int   `json:"user_sort_order"`
}

func (c Reorder) Snapshot(input json.RawMessage) (any, error) {
	var in struct {
		ProjectID  int64   `json:"project_id"`
		TagName    string  `json:"tag_name"`
		OrderedIDs []int64 `json:"ordered_request_ids"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	rows, err := c.tx.Query(`
		SELECT rt.id, rt.user_sort_order FROM request_tags rt
		JOIN tags t ON t.id = rt.tag_id
		WHERE t.project_id = ? AND t.name = ?`, in.ProjectID, in.TagName)
	if err != nil {
		return nil, fmt.Errorf("loading the current order: %w", err)
	}
	defer rows.Close()
	var saved []oldOrder
	for rows.Next() {
		var o oldOrder
		if err := rows.Scan(&o.ID, &o.UserSortOrder); err != nil {
			return nil, err
		}
		saved = append(saved, o)
	}
	return saved, rows.Err()
}

func (c Reorder) Run(input json.RawMessage) (any, error) {
	var in struct {
		ProjectID  int64   `json:"project_id"`
		TagName    string  `json:"tag_name"`
		OrderedIDs []int64 `json:"ordered_request_ids"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if len(in.OrderedIDs) == 0 {
		return nil, nil
	}

	query := `UPDATE request_tags SET user_sort_order = CASE request_id`
	args := make([]any, 0, len(in.OrderedIDs)*2)
	for index, id := range in.OrderedIDs {
		query += " WHEN ? THEN ?"
		args = append(args, id, index)
	}
	query += ` ELSE user_sort_order END WHERE tag_id =
	  (SELECT id FROM tags WHERE project_id = ? AND name = ?)`
	args = append(args, in.ProjectID, in.TagName)

	_, err := c.tx.Exec(query, args...)
	return nil, err
}

func (c Reorder) Undo(snapshot, _ json.RawMessage) error {
	var saved []oldOrder
	if err := json.Unmarshal(snapshot, &saved); err != nil {
		return err
	}
	query := `UPDATE request_tags SET user_sort_order = CASE id`
	args := make([]any, 0, len(saved)*2)
	for _, o := range saved {
		query += " WHEN ? THEN ?"
		args = append(args, o.ID, o.UserSortOrder)
	}
	query += " ELSE user_sort_order END"

	_, err := c.tx.Exec(query, args...)
	return err
}
