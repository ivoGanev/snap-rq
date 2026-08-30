package services

import (
	"database/sql"
	"fmt"

	"snap-rq/backend/models"
)

// EnvironmentService provides CRUD operations for environments.
type EnvironmentService struct {
	db *sql.DB
}

// NewEnvironmentService returns an EnvironmentService backed by the given database.
func NewEnvironmentService(db *sql.DB) *EnvironmentService {
	return &EnvironmentService{db: db}
}

// CreateEnvironment saves a new environment and returns it with its generated ID.
func (s *EnvironmentService) CreateEnvironment(env models.Environment) (models.Environment, error) {
	if env.ProjectID == 0 {
		return models.Environment{}, fmt.Errorf("project id is required")
	}

	result, err := s.db.Exec(
		`INSERT INTO environments (project_id, name, created_at) VALUES (?, ?, ?)`,
		env.ProjectID, env.Name, env.CreatedAt,
	)
	if err != nil {
		return models.Environment{}, fmt.Errorf("creating environment: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return models.Environment{}, fmt.Errorf("getting last insert id: %w", err)
	}

	env.ID = id
	return env, nil
}

// GetEnvironment retrieves a single environment by ID.
func (s *EnvironmentService) GetEnvironment(id int64) (models.Environment, error) {
	var env models.Environment
	row := s.db.QueryRow(
		`SELECT id, project_id, name, created_at FROM environments WHERE id = ?`,
		id,
	)
	err := row.Scan(&env.ID, &env.ProjectID, &env.Name, &env.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return models.Environment{}, fmt.Errorf("environment not found")
		}
		return models.Environment{}, fmt.Errorf("getting environment: %w", err)
	}
	return env, nil
}

// GetEnvironmentsForProject returns all environments for a project, newest first.
func (s *EnvironmentService) GetEnvironmentsForProject(projectID int64) ([]models.Environment, error) {
	rows, err := s.db.Query(
		`SELECT id, project_id, name, created_at FROM environments WHERE project_id = ? ORDER BY created_at DESC`,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing environments: %w", err)
	}
	defer rows.Close()

	var environments []models.Environment
	for rows.Next() {
		var env models.Environment
		if err := rows.Scan(&env.ID, &env.ProjectID, &env.Name, &env.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning environment: %w", err)
		}
		environments = append(environments, env)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating environments: %w", err)
	}

	return environments, nil
}

// UpdateEnvironment updates an existing environment.
func (s *EnvironmentService) UpdateEnvironment(env models.Environment) (models.Environment, error) {
	if env.ID == 0 {
		return models.Environment{}, fmt.Errorf("environment id is required")
	}

	_, err := s.db.Exec(
		`UPDATE environments SET project_id = ?, name = ?, created_at = ? WHERE id = ?`,
		env.ProjectID, env.Name, env.CreatedAt, env.ID,
	)
	if err != nil {
		return models.Environment{}, fmt.Errorf("updating environment: %w", err)
	}

	return env, nil
}

// DeleteEnvironment removes an environment and all of its variables by ID.
func (s *EnvironmentService) DeleteEnvironment(id int64) error {
	_, err := s.db.Exec(`DELETE FROM environments WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting environment: %w", err)
	}
	return nil
}
