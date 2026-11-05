package tags

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

//  — rename a tag across all its request links, in one command
// (this was a 4-call frontend loop). Snapshot saves the old tag row, its
// appearance and links; undo puts them back and removes the new tag.

type Rename struct{ tx *sql.Tx }

func NewRename(tx *sql.Tx) commands.Command { return &Rename{tx: tx} }

func (Rename) Info() commands.Info {
	return commands.Info{FunctionID: "tag.rename", Label: "Rename Tag"}
}

func (c Rename) Snapshot(input json.RawMessage) (any, error) {
	var in struct {
		ProjectID int64  `json:"project_id"`
		OldName   string `json:"old_name"`
		NewName   string `json:"new_name"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	// the old tag's full state — reuse Delete's snapshot shape
	del := Delete{tx: c.tx}
	raw, err := json.Marshal(map[string]any{
		"project_id": in.ProjectID, "tag_name": in.OldName,
	})
	if err != nil {
		return nil, err
	}
	return del.Snapshot(raw)
}

func (c Rename) Run(input json.RawMessage) (any, error) {
	var in struct {
		ProjectID int64  `json:"project_id"`
		OldName   string `json:"old_name"`
		NewName   string `json:"new_name"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	newName := strings.ToLower(strings.TrimSpace(in.NewName))
	if newName == "" || newName == strings.ToLower(in.OldName) {
		return nil, fmt.Errorf("invalid tag name")
	}

	// find or create the new tag, preserving the old appearance
	var oldAppearance struct{ Type, Value string }
	_ = c.tx.QueryRow(`
		SELECT COALESCE(ta.appearance_type, 'icon'), COALESCE(ta.appearance_value, 'default')
		FROM tags t LEFT JOIN tag_appearances ta ON ta.tag_id = t.id
		WHERE t.project_id = ? AND t.name = ?`, in.ProjectID, in.OldName,
	).Scan(&oldAppearance.Type, &oldAppearance.Value)

	var newTagID int64
	err := c.tx.QueryRow(
		`SELECT id FROM tags WHERE project_id = ? AND name = ?`, in.ProjectID, newName,
	).Scan(&newTagID)
	if err == sql.ErrNoRows {
		result, err := c.tx.Exec(
			`INSERT INTO tags (project_id, name) VALUES (?, ?)`, in.ProjectID, newName)
		if err != nil {
			return nil, err
		}
		newTagID, _ = result.LastInsertId()
		if _, err := c.tx.Exec(
			`INSERT INTO tag_appearances (tag_id, appearance_type, appearance_value) VALUES (?, ?, ?)`,
			newTagID, oldAppearance.Type, oldAppearance.Value,
		); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	// move every link from the old tag to the new one, then drop the old tag
	if _, err := c.tx.Exec(
		`UPDATE request_tags SET tag_id = ? WHERE tag_id =
		  (SELECT id FROM tags WHERE project_id = ? AND name = ?)`,
		newTagID, in.ProjectID, in.OldName,
	); err != nil {
		return nil, err
	}
	if _, err := c.tx.Exec(
		`DELETE FROM tags WHERE project_id = ? AND name = ?`, in.ProjectID, in.OldName,
	); err != nil {
		return nil, err
	}
	return models.Tag{ID: newTagID, ProjectID: in.ProjectID, Name: newName}, nil
}

func (c Rename) Undo(snapshot, _ json.RawMessage) error {
	var saved savedTag
	if err := json.Unmarshal(snapshot, &saved); err != nil {
		return err
	}
	// remove the new tag (cascades take its links and appearance), then
	// put the old tag, its appearance and its links back
	if _, err := c.tx.Exec(
		`DELETE FROM tags WHERE id <> ? AND project_id = ? AND name =
		  (SELECT name FROM tags WHERE id = ? LIMIT 1)`,
		saved.Tag.ID, saved.Tag.ProjectID, saved.Tag.ID,
	); err != nil {
		return err
	}
	del := Delete{tx: c.tx}
	raw, _ := json.Marshal(saved)
	return del.Undo(raw, nil)
}

func (Rename) Affected(_, result json.RawMessage) []commands.Affected {
	return []commands.Affected{{Component: "tag", IDs: commands.IDsOf(result, "id")}}
}
