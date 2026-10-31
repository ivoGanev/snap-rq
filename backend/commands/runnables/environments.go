package runnables

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
	"snap-rq/backend/store"
)

// CreateEnvironment saves a new environment. Undo removes it.
type CreateEnvironment struct{}

func (CreateEnvironment) Info() commands.Info {
	return commands.Info{
		FunctionID:      "environment.create",
		Label:           "Create Environment",
		UndoOperationID: "environment.delete",
	}
}

func (CreateEnvironment) Run(db store.Session, input json.RawMessage) (any, error) {
	var payload struct {
		ProjectID int64  `json:"project_id"`
		Name      string `json:"name"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, err
	}
	if payload.ProjectID == 0 {
		return nil, errors.New("project id is required")
	}

	created := time.Now().UTC()
	id, err := db.Insert("environments", map[string]any{
		"project_id": payload.ProjectID,
		"name":       payload.Name,
		"created_at": created.Format("2006-01-02 15:04:05"),
	})
	if err != nil {
		return nil, err
	}
	return models.Environment{ID: id, ProjectID: payload.ProjectID, Name: payload.Name, CreatedAt: created}, nil
}

// DeleteEnvironment removes an environment together with its variables;
// the default environment is refused. Undo puts all of it back.
type DeleteEnvironment struct{}

func (DeleteEnvironment) Info() commands.Info {
	return commands.Info{
		FunctionID:      "environment.delete",
		Label:           "Delete Environment",
		UndoOperationID: "environment.create",
	}
}

func (DeleteEnvironment) Run(db store.Session, input json.RawMessage) (any, error) {
	var payload struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, err
	}
	if payload.ID == 0 {
		return nil, errors.New("environment id is required")
	}

	var name string
	if err := db.QueryRow(
		`SELECT name FROM environments WHERE id = ?`, payload.ID,
	).Scan(&name); err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.New("environment not found")
		}
		return nil, fmt.Errorf("loading environment: %w", err)
	}
	if strings.EqualFold(name, "Default") {
		return nil, errors.New("cannot delete the default environment")
	}

	return nil, db.Delete("environments", payload.ID)
}

// CreateVariable adds one variable to an environment. Undo removes it.
type CreateVariable struct{}

func (CreateVariable) Info() commands.Info {
	return commands.Info{
		FunctionID:      "variable.create",
		Label:           "Create Variable",
		UndoOperationID: "variable.delete",
	}
}

func (CreateVariable) Run(db store.Session, input json.RawMessage) (any, error) {
	var payload struct {
		EnvironmentID int64  `json:"environment_id"`
		Key           string `json:"key"`
		Value         string `json:"value"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, err
	}
	if payload.EnvironmentID == 0 {
		return nil, errors.New("environment id is required")
	}

	id, err := db.Insert("environment_variables", map[string]any{
		"environment_id": payload.EnvironmentID,
		"key":            payload.Key,
		"value":          payload.Value,
	})
	if err != nil {
		return nil, err
	}
	return models.EnvironmentVariable{
		ID: id, EnvironmentID: payload.EnvironmentID, Key: payload.Key, Value: payload.Value,
	}, nil
}

// DeleteVariable removes one variable. Undo puts the exact key/value row
// back as it was.
type DeleteVariable struct{}

func (DeleteVariable) Info() commands.Info {
	return commands.Info{
		FunctionID:      "variable.delete",
		Label:           "Delete Variable",
		UndoOperationID: "variable.create",
	}
}

func (DeleteVariable) Run(db store.Session, input json.RawMessage) (any, error) {
	var payload struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, err
	}
	if payload.ID == 0 {
		return nil, errors.New("variable id is required")
	}

	// The engine saves a copy of the row before deleting it; a missing row is
	// the command's own error to report.
	var one int
	if err := db.QueryRow(
		`SELECT 1 FROM environment_variables WHERE id = ?`, payload.ID,
	).Scan(&one); err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.New("variable not found")
		}
		return nil, fmt.Errorf("loading variable: %w", err)
	}

	return nil, db.Delete("environment_variables", payload.ID)
}
