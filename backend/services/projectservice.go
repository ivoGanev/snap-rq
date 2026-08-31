package services

import (
	"database/sql"
	"fmt"

	"snap-rq/backend/models"
)

// ProjectService provides CRUD operations for projects.
type ProjectService struct {
	db *sql.DB
}

// NewProjectService returns a ProjectService backed by the given database.
func NewProjectService(db *sql.DB) *ProjectService {
	return &ProjectService{db: db}
}

// CreateProject saves a new project and returns it with its generated ID.
func (s *ProjectService) CreateProject(project models.Project) (models.Project, error) {
	result, err := s.db.Exec(
		"INSERT INTO projects (profile_id, name) VALUES (?, ?)",
		project.ProfileID, project.Name,
	)
	if err != nil {
		return models.Project{}, fmt.Errorf("creating project: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return models.Project{}, fmt.Errorf("getting last insert id: %w", err)
	}
	project.ID = id
	return project, nil
}

// GetProject retrieves a single project by ID.
func (s *ProjectService) GetProject(id int64) (models.Project, error) {
	var project models.Project
	row := s.db.QueryRow("SELECT id, profile_id, name FROM projects WHERE id = ?", id)
	err := row.Scan(&project.ID, &project.ProfileID, &project.Name)
	if err != nil {
		if err == sql.ErrNoRows {
			return models.Project{}, fmt.Errorf("project not found")
		}
		return models.Project{}, fmt.Errorf("getting project: %w", err)
	}
	return project, nil
}

// GetAllProjects returns all projects ordered by name.
func (s *ProjectService) GetAllProjects() ([]models.Project, error) {
	rows, err := s.db.Query("SELECT id, profile_id, name FROM projects ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("listing projects: %w", err)
	}
	defer rows.Close()

	var projects []models.Project
	for rows.Next() {
		var project models.Project
		if err := rows.Scan(&project.ID, &project.ProfileID, &project.Name); err != nil {
			return nil, fmt.Errorf("scanning project: %w", err)
		}
		projects = append(projects, project)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating projects: %w", err)
	}

	return projects, nil
}

// GetProjectsForProfile returns all projects for a given profile ID.
func (s *ProjectService) GetProjectsForProfile(profileID int64) ([]models.Project, error) {
	rows, err := s.db.Query(
		"SELECT id, profile_id, name FROM projects WHERE profile_id = ? ORDER BY name",
		profileID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing projects: %w", err)
	}
	defer rows.Close()

	var projects []models.Project
	for rows.Next() {
		var project models.Project
		if err := rows.Scan(&project.ID, &project.ProfileID, &project.Name); err != nil {
			return nil, fmt.Errorf("scanning project: %w", err)
		}
		projects = append(projects, project)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating projects: %w", err)
	}

	return projects, nil
}

// UpdateProject updates an existing project.
func (s *ProjectService) UpdateProject(project models.Project) (models.Project, error) {
	if project.ID == 0 {
		return models.Project{}, fmt.Errorf("project id is required")
	}
	_, err := s.db.Exec(
		"UPDATE projects SET profile_id = ?, name = ? WHERE id = ?",
		project.ProfileID, project.Name, project.ID,
	)
	if err != nil {
		return models.Project{}, fmt.Errorf("updating project: %w", err)
	}
	return project, nil
}

// DeleteProject removes a project by ID. The database cascades the deletion
// to collections, requests, responses, and favourite items referencing those
// requests. The IDs of the deleted requests are returned so the frontend can
// clean up its runtime selection state.
func (s *ProjectService) DeleteProject(id int64) ([]int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.Query(`
		SELECT hr.id FROM http_requests hr
		JOIN collections c ON hr.collection_id = c.id
		WHERE c.project_id = ?`, id)
	if err != nil {
		return nil, fmt.Errorf("listing requests: %w", err)
	}

	var requestIDs []int64
	for rows.Next() {
		var requestID int64
		if err := rows.Scan(&requestID); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scanning request id: %w", err)
		}
		requestIDs = append(requestIDs, requestID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterating request ids: %w", err)
	}
	rows.Close()

	_, err = tx.Exec("DELETE FROM projects WHERE id = ?", id)
	if err != nil {
		return nil, fmt.Errorf("deleting project: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing transaction: %w", err)
	}

	return requestIDs, nil
}
