package services

import (
	"database/sql"
	"fmt"

	"snap-rq/backend/models"
)

// CollectionService provides CRUD operations for collections.
type CollectionService struct {
	db *sql.DB
}

// NewCollectionService returns a CollectionService backed by the given database.
func NewCollectionService(db *sql.DB) *CollectionService {
	return &CollectionService{db: db}
}

// CreateCollection saves a new collection and returns it with its generated ID.
func (s *CollectionService) CreateCollection(collection models.Collection) (models.Collection, error) {
	result, err := s.db.Exec(
		"INSERT INTO collections (project_id, name, icon_id) VALUES (?, ?, ?)",
		collection.ProjectID, collection.Name, collection.IconID,
	)
	if err != nil {
		return models.Collection{}, fmt.Errorf("creating collection: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return models.Collection{}, fmt.Errorf("getting last insert id: %w", err)
	}
	collection.ID = id
	return collection, nil
}

// GetCollection retrieves a single collection by ID.
func (s *CollectionService) GetCollection(id int64) (models.Collection, error) {
	var collection models.Collection
	row := s.db.QueryRow("SELECT id, project_id, name, icon_id FROM collections WHERE id = ?", id)
	err := row.Scan(&collection.ID, &collection.ProjectID, &collection.Name, &collection.IconID)
	if err != nil {
		if err == sql.ErrNoRows {
			return models.Collection{}, fmt.Errorf("collection not found")
		}
		return models.Collection{}, fmt.Errorf("getting collection: %w", err)
	}
	return collection, nil
}

// GetCollectionsForProject returns all collections for a given project ID.
func (s *CollectionService) GetCollectionsForProject(projectID int64) ([]models.Collection, error) {
	rows, err := s.db.Query(
		"SELECT id, project_id, name, icon_id FROM collections WHERE project_id = ? ORDER BY name",
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing collections: %w", err)
	}
	defer rows.Close()

	var collections []models.Collection
	for rows.Next() {
		var collection models.Collection
		if err := rows.Scan(&collection.ID, &collection.ProjectID, &collection.Name, &collection.IconID); err != nil {
			return nil, fmt.Errorf("scanning collection: %w", err)
		}
		collections = append(collections, collection)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating collections: %w", err)
	}

	return collections, nil
}

// GetAllCollections returns all collections ordered by name.
func (s *CollectionService) GetAllCollections() ([]models.Collection, error) {
	rows, err := s.db.Query("SELECT id, project_id, name, icon_id FROM collections ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("listing collections: %w", err)
	}
	defer rows.Close()

	var collections []models.Collection
	for rows.Next() {
		var collection models.Collection
		if err := rows.Scan(&collection.ID, &collection.ProjectID, &collection.Name, &collection.IconID); err != nil {
			return nil, fmt.Errorf("scanning collection: %w", err)
		}
		collections = append(collections, collection)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating collections: %w", err)
	}

	return collections, nil
}

// UpdateCollection updates an existing collection.
func (s *CollectionService) UpdateCollection(collection models.Collection) (models.Collection, error) {
	if collection.ID == 0 {
		return models.Collection{}, fmt.Errorf("collection id is required")
	}
	_, err := s.db.Exec(
		"UPDATE collections SET project_id = ?, name = ?, icon_id = ? WHERE id = ?",
		collection.ProjectID, collection.Name, collection.IconID, collection.ID,
	)
	if err != nil {
		return models.Collection{}, fmt.Errorf("updating collection: %w", err)
	}
	return collection, nil
}

// DeleteCollection removes a collection by ID. The database cascades the
// deletion to all requests in the collection, their responses, and any
// favourite items referencing those requests. The IDs of the deleted requests
// are returned so the frontend can clean up its runtime selection state.
func (s *CollectionService) DeleteCollection(id int64) ([]int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.Query("SELECT id FROM http_requests WHERE collection_id = ?", id)
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

	_, err = tx.Exec("DELETE FROM collections WHERE id = ?", id)
	if err != nil {
		return nil, fmt.Errorf("deleting collection: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing transaction: %w", err)
	}

	return requestIDs, nil
}
