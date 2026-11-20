package environments

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"snap-rq/backend/backup"
	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// delete.go — remove an environment with its variables; the default
// environment is refused. Snapshot saves the rows; undo re-inserts them.

type Delete struct{ tx *sql.Tx }

func NewDelete(tx *sql.Tx) commands.Command { return &Delete{tx: tx} }

func (Delete) Info() commands.Info {
	return commands.Info{FunctionID: "environment.delete", Label: "Delete Environment"}
}

type savedEnvironment struct {
	Environment models.Environment           `json:"environment"`
	Variables   []models.EnvironmentVariable `json:"variables"`
}

func (c Delete) Snapshot(input json.RawMessage) (any, error) {
	var in struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}

	saved := savedEnvironment{}
	err := c.tx.QueryRow(
		`SELECT id, project_id, name, created_at FROM environments WHERE id = ?`, in.ID,
	).Scan(&saved.Environment.ID, &saved.Environment.ProjectID, &saved.Environment.Name,
		&saved.Environment.CreatedAt)
	if err != nil {
		return nil, errors.New("environment not found")
	}

	rows, err := c.tx.Query(
		`SELECT id, environment_id, key, value, secret FROM environment_variables
		 WHERE environment_id = ? ORDER BY id`, in.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var v models.EnvironmentVariable
		// A secret's value stays encrypted in the snapshot.
		if err := rows.Scan(&v.ID, &v.EnvironmentID, &v.Key, &v.Value, &v.Secret); err != nil {
			return nil, err
		}
		saved.Variables = append(saved.Variables, v)
	}
	return saved, rows.Err()
}

func (c Delete) Run(input json.RawMessage) (any, error) {
	var in struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if in.ID == 0 {
		return nil, errors.New("environment id is required")
	}

	var name string
	if err := c.tx.QueryRow(
		`SELECT name FROM environments WHERE id = ?`, in.ID,
	).Scan(&name); err != nil {
		return nil, errors.New("environment not found")
	}
	if strings.EqualFold(name, "Default") {
		return nil, errors.New("cannot delete the default environment")
	}
	_, err := c.tx.Exec(`DELETE FROM environments WHERE id = ?`, in.ID)
	return nil, err
}

func (c Delete) Undo(snapshot, _ json.RawMessage) error {
	var saved savedEnvironment
	if err := json.Unmarshal(snapshot, &saved); err != nil {
		return err
	}
	env := saved.Environment
	if _, err := c.tx.Exec(
		`INSERT INTO environments (id, project_id, name, created_at) VALUES (?, ?, ?, ?)`,
		env.ID, env.ProjectID, env.Name, env.CreatedAt,
	); err != nil {
		return err
	}
	for _, v := range saved.Variables {
		if _, err := c.tx.Exec(
			`INSERT INTO environment_variables (id, environment_id, key, value, secret) VALUES (?, ?, ?, ?, ?)`,
			v.ID, v.EnvironmentID, v.Key, v.Value, v.Secret,
		); err != nil {
			return err
		}
	}
	return nil
}

func (Delete) Changes(input, _ json.RawMessage) []commands.RowChange {
	return []commands.RowChange{{Component: "environment", IDs: commands.IDsOf(input, "id")}}
}

// BackupBefore: deleting an environment that holds variables takes a backup first.
func (c Delete) BackupBefore(input json.RawMessage) (commands.BackupNeed, bool, error) {
	var in struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return commands.BackupNeed{}, false, err
	}
	var name string
	var count int
	if err := c.tx.QueryRow(`
		SELECT e.name, (SELECT COUNT(*) FROM environment_variables v WHERE v.environment_id = e.id)
		FROM environments e WHERE e.id = ?`, in.ID).Scan(&name, &count); err != nil {
		return commands.BackupNeed{}, false, err
	}
	return commands.BackupNeed{
		Trigger: string(backup.TriggerDeleteEnvironment),
		Reason:  fmt.Sprintf("before Delete environment %q (%d variables)", name, count),
		Items:   count,
	}, count > 0, nil
}
