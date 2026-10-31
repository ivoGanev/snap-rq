package tags

import (
	"database/sql"
	"encoding/json"

	"snap-rq/backend/commands"
)

//  — set (or clear) the colour on many requests' links in one
// tag. Snapshot saves the old colours; undo restores them.

type SetColor struct{ tx *sql.Tx }

func NewSetColor(tx *sql.Tx) commands.Command { return &SetColor{tx: tx} }

func (SetColor) Info() commands.Info {
	return commands.Info{FunctionID: "tag.set_color", Label: "Set Tag Colours"}
}

func (c SetColor) Snapshot(input json.RawMessage) (any, error) {
	var in struct {
		ProjectID  int64   `json:"project_id"`
		TagName    string  `json:"tag_name"`
		RequestIDs []int64 `json:"request_ids"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	placeholders := ""
	args := []any{in.ProjectID, in.TagName}
	for i, id := range in.RequestIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}
	rows, err := c.tx.Query(`
		SELECT rt.id, rt.user_color FROM request_tags rt
		JOIN tags t ON t.id = rt.tag_id
		WHERE t.project_id = ? AND t.name = ? AND rt.request_id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var saved []oldColor
	for rows.Next() {
		var o oldColor
		if err := rows.Scan(&o.ID, &o.UserColor); err != nil {
			return nil, err
		}
		saved = append(saved, o)
	}
	return saved, rows.Err()
}

type oldColor struct {
	ID        int64   `json:"id"`
	UserColor *string `json:"user_color"`
}

func (c SetColor) Run(input json.RawMessage) (any, error) {
	var in struct {
		ProjectID  int64   `json:"project_id"`
		TagName    string  `json:"tag_name"`
		RequestIDs []int64 `json:"request_ids"`
		Color      *string `json:"color"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if len(in.RequestIDs) == 0 {
		return nil, nil
	}

	placeholders := ""
	args := make([]any, 0, len(in.RequestIDs)+3)
	if in.Color != nil && *in.Color != "" {
		args = append(args, *in.Color)
	} else {
		args = append(args, nil)
	}
	args = append(args, in.ProjectID, in.TagName)
	for i, id := range in.RequestIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}
	_, err := c.tx.Exec(`
		UPDATE request_tags SET user_color = ? WHERE tag_id =
		  (SELECT id FROM tags WHERE project_id = ? AND name = ?)
		  AND request_id IN (`+placeholders+`)`, args...)
	return nil, err
}

func (c SetColor) Undo(snapshot, _ json.RawMessage) error {
	var saved []oldColor
	if err := json.Unmarshal(snapshot, &saved); err != nil {
		return err
	}
	for _, o := range saved {
		if _, err := c.tx.Exec(
			`UPDATE request_tags SET user_color = ? WHERE id = ?`, o.UserColor, o.ID,
		); err != nil {
			return err
		}
	}
	return nil
}
