package scripts

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// rename.go — rename a script in place: its id, content, links and
// appearance stay. Renaming onto a name the project already uses fails
// (unlike tags, two scripts are never merged). Snapshot saves the old name;
// undo puts it back.

type Rename struct{ tx *sql.Tx }

func NewRename(tx *sql.Tx) commands.Command { return &Rename{tx: tx} }

func (Rename) Info() commands.Info {
	return commands.Info{FunctionID: "script.rename", Label: "Rename Script"}
}

type renameInput struct {
	ProjectID int64  `json:"project_id"`
	OldName   string `json:"old_name"`
	NewName   string `json:"new_name"`
}

func (c Rename) Snapshot(input json.RawMessage) (any, error) {
	var in renameInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	var id int64
	if err := c.tx.QueryRow(
		`SELECT id FROM scripts WHERE project_id = ? AND name = ?`, in.ProjectID, in.OldName,
	).Scan(&id); err != nil {
		return nil, fmt.Errorf("script not found: %w", err)
	}
	return map[string]any{"id": id, "name": in.OldName}, nil
}

func (c Rename) Run(input json.RawMessage) (any, error) {
	var in renameInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	newName := strings.TrimSpace(in.NewName)
	if newName == "" {
		return nil, fmt.Errorf("script name is required")
	}

	var id int64
	if err := c.tx.QueryRow(
		`SELECT id FROM scripts WHERE project_id = ? AND name = ?`, in.ProjectID, in.OldName,
	).Scan(&id); err != nil {
		return nil, fmt.Errorf("script not found: %w", err)
	}
	if newName == in.OldName {
		return models.Script{ID: id, ProjectID: in.ProjectID, Name: newName}, nil
	}

	var taken int
	if err := c.tx.QueryRow(
		`SELECT COUNT(*) FROM scripts WHERE project_id = ? AND name = ?`, in.ProjectID, newName,
	).Scan(&taken); err != nil {
		return nil, err
	}
	if taken > 0 {
		return nil, fmt.Errorf("a script named %q already exists", newName)
	}

	if _, err := c.tx.Exec(`UPDATE scripts SET name = ? WHERE id = ?`, newName, id); err != nil {
		return nil, fmt.Errorf("renaming script: %w", err)
	}
	return models.Script{ID: id, ProjectID: in.ProjectID, Name: newName}, nil
}

func (c Rename) Undo(snapshot, _ json.RawMessage) error {
	var old struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(snapshot, &old); err != nil {
		return err
	}
	_, err := c.tx.Exec(`UPDATE scripts SET name = ? WHERE id = ?`, old.Name, old.ID)
	return err
}

func (Rename) Changes(_, result json.RawMessage) []commands.RowChange {
	return []commands.RowChange{{Component: "script", IDs: commands.IDsOf(result, "id")}}
}
