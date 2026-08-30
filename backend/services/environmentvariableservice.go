package services

import (
	"database/sql"
	"fmt"

	"snap-rq/backend/models"
)

// EnvironmentVariableService provides CRUD operations for environment variables.
type EnvironmentVariableService struct {
	db *sql.DB
}

// NewEnvironmentVariableService returns an EnvironmentVariableService backed by the given database.
func NewEnvironmentVariableService(db *sql.DB) *EnvironmentVariableService {
	return &EnvironmentVariableService{db: db}
}

// CreateVariable saves a new variable and returns it with its generated ID.
func (s *EnvironmentVariableService) CreateVariable(v models.EnvironmentVariable) (models.EnvironmentVariable, error) {
	if v.EnvironmentID == 0 {
		return models.EnvironmentVariable{}, fmt.Errorf("environment id is required")
	}

	result, err := s.db.Exec(
		`INSERT INTO environment_variables (environment_id, key, value) VALUES (?, ?, ?)`,
		v.EnvironmentID, v.Key, v.Value,
	)
	if err != nil {
		return models.EnvironmentVariable{}, fmt.Errorf("creating variable: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return models.EnvironmentVariable{}, fmt.Errorf("getting last insert id: %w", err)
	}

	v.ID = id
	return v, nil
}

// GetVariable retrieves a single variable by ID.
func (s *EnvironmentVariableService) GetVariable(id int64) (models.EnvironmentVariable, error) {
	var v models.EnvironmentVariable
	row := s.db.QueryRow(
		`SELECT id, environment_id, key, value FROM environment_variables WHERE id = ?`,
		id,
	)
	err := row.Scan(&v.ID, &v.EnvironmentID, &v.Key, &v.Value)
	if err != nil {
		if err == sql.ErrNoRows {
			return models.EnvironmentVariable{}, fmt.Errorf("variable not found")
		}
		return models.EnvironmentVariable{}, fmt.Errorf("getting variable: %w", err)
	}
	return v, nil
}

// GetVariablesForEnvironment returns all variables for an environment, ordered by key.
func (s *EnvironmentVariableService) GetVariablesForEnvironment(environmentID int64) ([]models.EnvironmentVariable, error) {
	rows, err := s.db.Query(
		`SELECT id, environment_id, key, value FROM environment_variables WHERE environment_id = ? ORDER BY key`,
		environmentID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing variables: %w", err)
	}
	defer rows.Close()

	var variables []models.EnvironmentVariable
	for rows.Next() {
		var v models.EnvironmentVariable
		if err := rows.Scan(&v.ID, &v.EnvironmentID, &v.Key, &v.Value); err != nil {
			return nil, fmt.Errorf("scanning variable: %w", err)
		}
		variables = append(variables, v)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating variables: %w", err)
	}

	return variables, nil
}

// UpdateVariable updates an existing variable.
func (s *EnvironmentVariableService) UpdateVariable(v models.EnvironmentVariable) (models.EnvironmentVariable, error) {
	if v.ID == 0 {
		return models.EnvironmentVariable{}, fmt.Errorf("variable id is required")
	}

	_, err := s.db.Exec(
		`UPDATE environment_variables SET environment_id = ?, key = ?, value = ? WHERE id = ?`,
		v.EnvironmentID, v.Key, v.Value, v.ID,
	)
	if err != nil {
		return models.EnvironmentVariable{}, fmt.Errorf("updating variable: %w", err)
	}

	return v, nil
}

// DeleteVariable removes a variable by ID.
func (s *EnvironmentVariableService) DeleteVariable(id int64) error {
	_, err := s.db.Exec(`DELETE FROM environment_variables WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting variable: %w", err)
	}
	return nil
}
