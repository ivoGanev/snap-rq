package tags

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

//  — remove a tag with its links and appearance. Snapshot saves
// them; undo re-inserts everything.

type Delete struct{ tx *sql.Tx }

func NewDelete(tx *sql.Tx) commands.Command { return &Delete{tx: tx} }

func (Delete) Info() commands.Info {
	return commands.Info{FunctionID: "tag.delete", Label: "Delete Tag"}
}

type savedTag struct {
	Tag             models.Tag  `json:"tag"`
	AppearanceType  string      `json:"appearance_type"`
	AppearanceValue string      `json:"appearance_value"`
	Links           []savedLink `json:"links"`
}

func (c Delete) Snapshot(input json.RawMessage) (any, error) {
	var in struct {
		ProjectID int64  `json:"project_id"`
		TagName   string `json:"tag_name"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}

	saved := savedTag{}
	err := c.tx.QueryRow(`
		SELECT t.id, t.project_id, t.name,
		       COALESCE(ta.appearance_type, ''), COALESCE(ta.appearance_value, '')
		FROM tags t
		LEFT JOIN tag_appearances ta ON ta.tag_id = t.id
		WHERE t.project_id = ? AND t.name = ?`, in.ProjectID, in.TagName,
	).Scan(&saved.Tag.ID, &saved.Tag.ProjectID, &saved.Tag.Name,
		&saved.AppearanceType, &saved.AppearanceValue)
	if err != nil {
		return nil, fmt.Errorf("tag not found: %w", err)
	}

	rows, err := c.tx.Query(`
		SELECT id, request_id, tag_id, user_sort_order, user_color
		FROM request_tags WHERE tag_id = ? ORDER BY id`, saved.Tag.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var link savedLink
		if err := rows.Scan(&link.ID, &link.RequestID, &link.TagID,
			&link.UserSortOrder, &link.UserColor); err != nil {
			return nil, err
		}
		saved.Links = append(saved.Links, link)
	}
	return saved, rows.Err()
}

func (c Delete) Run(input json.RawMessage) (any, error) {
	var in struct {
		ProjectID int64  `json:"project_id"`
		TagName   string `json:"tag_name"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	_, err := c.tx.Exec(
		`DELETE FROM tags WHERE project_id = ? AND name = ?`, in.ProjectID, in.TagName)
	return nil, err
}

func (c Delete) Undo(snapshot, _ json.RawMessage) error {
	var saved savedTag
	if err := json.Unmarshal(snapshot, &saved); err != nil {
		return err
	}
	if _, err := c.tx.Exec(
		`INSERT INTO tags (id, project_id, name) VALUES (?, ?, ?)`,
		saved.Tag.ID, saved.Tag.ProjectID, saved.Tag.Name,
	); err != nil {
		return err
	}
	if saved.AppearanceType != "" {
		if _, err := c.tx.Exec(
			`INSERT INTO tag_appearances (tag_id, appearance_type, appearance_value) VALUES (?, ?, ?)`,
			saved.Tag.ID, saved.AppearanceType, saved.AppearanceValue,
		); err != nil {
			return err
		}
	}
	for _, link := range saved.Links {
		if _, err := c.tx.Exec(`
			INSERT INTO request_tags (id, request_id, tag_id, user_sort_order, user_color)
			VALUES (?, ?, ?, ?, ?)`,
			link.ID, link.RequestID, link.TagID, link.UserSortOrder, link.UserColor,
		); err != nil {
			return err
		}
	}
	return nil
}
