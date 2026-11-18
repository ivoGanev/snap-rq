package favourites

import (
	"database/sql"
	"fmt"
)

// name.go — names must be unique inside a project, compared without regard
// to case. The database enforces it with an index; this check gives a
// readable error before the insert or rename reaches it.

func ensureNameFree(tx *sql.Tx, projectID int64, name string, exceptID int64) error {
	var found int
	err := tx.QueryRow(
		`SELECT COUNT(*) FROM favourite_collections WHERE project_id = ? AND name = ? COLLATE NOCASE AND id != ?`,
		projectID, name, exceptID,
	).Scan(&found)
	if err != nil {
		return fmt.Errorf("checking favourite name: %w", err)
	}
	if found > 0 {
		return fmt.Errorf("a favourite named %q already exists in this project", name)
	}
	return nil
}
