package tags

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

//  — add a tag to a request, creating the tag if it does not exist.
// Snapshot saves whether the tag and the link existed; undo removes what
// was created.

type Add struct{ tx *sql.Tx }

func NewAdd(tx *sql.Tx) commands.Command { return &Add{tx: tx} }

func (Add) Info() commands.Info {
	return commands.Info{FunctionID: "tag.add", Label: "Add Tag"}
}

func (c Add) Snapshot(input json.RawMessage) (any, error) {
	var in struct {
		RequestID int64  `json:"request_id"`
		ProjectID int64  `json:"project_id"`
		TagName   string `json:"tag_name"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	name := strings.ToLower(strings.TrimSpace(in.TagName))

	var tagID int64
	tagExists := false
	if err := c.tx.QueryRow(
		`SELECT id FROM tags WHERE project_id = ? AND name = ?`, in.ProjectID, name,
	).Scan(&tagID); err == nil {
		tagExists = true
	}

	linkExists := false
	if tagExists {
		var one int
		if err := c.tx.QueryRow(
			`SELECT 1 FROM request_tags WHERE request_id = ? AND tag_id = ?`, in.RequestID, tagID,
		).Scan(&one); err == nil {
			linkExists = true
		}
	}
	return map[string]any{"tag_id": tagID, "tag_existed": tagExists, "link_existed": linkExists}, nil
}

func (c Add) Run(input json.RawMessage) (any, error) {
	var in struct {
		RequestID int64  `json:"request_id"`
		ProjectID int64  `json:"project_id"`
		TagName   string `json:"tag_name"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	name := strings.ToLower(strings.TrimSpace(in.TagName))
	if name == "" {
		return nil, fmt.Errorf("tag name is required")
	}

	var tagID int64
	err := c.tx.QueryRow(
		`SELECT id FROM tags WHERE project_id = ? AND name = ?`, in.ProjectID, name,
	).Scan(&tagID)
	if err == sql.ErrNoRows {
		result, err := c.tx.Exec(
			`INSERT INTO tags (project_id, name) VALUES (?, ?)`, in.ProjectID, name)
		if err != nil {
			return nil, fmt.Errorf("creating tag: %w", err)
		}
		tagID, _ = result.LastInsertId()
	} else if err != nil {
		return nil, err
	}

	if in.RequestID != 0 {
		if _, err := c.tx.Exec(
			`INSERT OR IGNORE INTO request_tags (request_id, tag_id) VALUES (?, ?)`,
			in.RequestID, tagID); err != nil {
			return nil, fmt.Errorf("tagging request: %w", err)
		}
	}
	return models.Tag{ID: tagID, ProjectID: in.ProjectID, Name: name}, nil
}

func (c Add) Undo(snapshot, _ json.RawMessage) error {
	var old struct {
		TagID       int64 `json:"tag_id"`
		TagExisted  bool  `json:"tag_existed"`
		LinkExisted bool  `json:"link_existed"`
	}
	if err := json.Unmarshal(snapshot, &old); err != nil {
		return err
	}
	if !old.LinkExisted {
		if _, err := c.tx.Exec(
			`DELETE FROM request_tags WHERE tag_id = ?`, old.TagID); err != nil {
			return err
		}
	}
	if !old.TagExisted {
		if _, err := c.tx.Exec(
			`DELETE FROM tags WHERE id = ?`, old.TagID); err != nil {
			return err
		}
	}
	return nil
}

func (Add) Affected(_, result json.RawMessage) []commands.Affected {
	return []commands.Affected{{Component: "tag", IDs: commands.IDsOf(result, "id")}}
}
