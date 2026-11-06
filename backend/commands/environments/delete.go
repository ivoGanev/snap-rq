package environments

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

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
		`SELECT id, environment_id, key, value FROM environment_variables
		 WHERE environment_id = ? ORDER BY id`, in.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var v models.EnvironmentVariable
		if err := rows.Scan(&v.ID, &v.EnvironmentID, &v.Key, &v.Value); err != nil {
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
			`INSERT INTO environment_variables (id, environment_id, key, value) VALUES (?, ?, ?, ?)`,
			v.ID, v.EnvironmentID, v.Key, v.Value,
		); err != nil {
			return err
		}
	}
	return nil
}

func (Delete) Changes(input, _ json.RawMessage) []commands.RowChange {
	return []commands.RowChange{{Component: "environment", IDs: commands.IDsOf(input, "id")}}
}
