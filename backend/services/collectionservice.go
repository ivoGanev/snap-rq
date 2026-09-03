package services

import (
	"database/sql"
	"fmt"

	"snap-rq/backend/models"
)

// CollectionService provides CRUD operations for collections and their
// associated appearance rows.
type CollectionService struct {
	db *sql.DB
}

// NewCollectionService returns a CollectionService backed by the given database.
func NewCollectionService(db *sql.DB) *CollectionService {
	return &CollectionService{db: db}
}

// CreateCollection saves a new collection and returns it with its generated ID.
// Every new collection gets a default appearance row pointing to the default icon.
func (s *CollectionService) CreateCollection(collection models.Collection) (models.Collection, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return models.Collection{}, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.Exec(
		"INSERT INTO collections (project_id, name) VALUES (?, ?)",
		collection.ProjectID, collection.Name,
	)
	if err != nil {
		return models.Collection{}, fmt.Errorf("creating collection: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return models.Collection{}, fmt.Errorf("getting last insert id: %w", err)
	}
	collection.ID = id

	defaultAppearance := models.DefaultCollectionAppearance()
	if _, err := tx.Exec(
		"INSERT INTO collection_appearances (collection_id, appearance_type, appearance_value) VALUES (?, ?, ?)",
		collection.ID, defaultAppearance.AppearanceType, defaultAppearance.AppearanceValue,
	); err != nil {
		return models.Collection{}, fmt.Errorf("creating collection appearance: %w", err)
	}
	collection.Appearance = defaultAppearance

	if err := tx.Commit(); err != nil {
		return models.Collection{}, fmt.Errorf("committing transaction: %w", err)
	}

	return collection, nil
}

// GetCollection retrieves a single collection by ID, including its appearance.
func (s *CollectionService) GetCollection(id int64) (models.Collection, error) {
	var collection models.Collection
	row := s.db.QueryRow(`
		SELECT c.id, c.project_id, c.name, COALESCE(ca.appearance_type, 'icon'), COALESCE(ca.appearance_value, 'default')
		FROM collections c
		LEFT JOIN collection_appearances ca ON ca.collection_id = c.id
		WHERE c.id = ?`, id)
	err := row.Scan(&collection.ID, &collection.ProjectID, &collection.Name, &collection.Appearance.AppearanceType, &collection.Appearance.AppearanceValue)
	if err != nil {
		if err == sql.ErrNoRows {
			return models.Collection{}, fmt.Errorf("collection not found")
		}
		return models.Collection{}, fmt.Errorf("getting collection: %w", err)
	}
	collection.Appearance.CollectionID = collection.ID
	return collection, nil
}

// GetCollectionsForProject returns all collections for a given project ID, including their appearances.
func (s *CollectionService) GetCollectionsForProject(projectID int64) ([]models.Collection, error) {
	rows, err := s.db.Query(`
		SELECT c.id, c.project_id, c.name, COALESCE(ca.appearance_type, 'icon'), COALESCE(ca.appearance_value, 'default')
		FROM collections c
		LEFT JOIN collection_appearances ca ON ca.collection_id = c.id
		WHERE c.project_id = ?
		ORDER BY c.name`,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing collections: %w", err)
	}
	defer rows.Close()

	var collections []models.Collection
	for rows.Next() {
		var collection models.Collection
		if err := rows.Scan(&collection.ID, &collection.ProjectID, &collection.Name, &collection.Appearance.AppearanceType, &collection.Appearance.AppearanceValue); err != nil {
			return nil, fmt.Errorf("scanning collection: %w", err)
		}
		collection.Appearance.CollectionID = collection.ID
		collections = append(collections, collection)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating collections: %w", err)
	}

	return collections, nil
}

// GetAllCollections returns all collections ordered by name, including their appearances.
func (s *CollectionService) GetAllCollections() ([]models.Collection, error) {
	rows, err := s.db.Query(`
		SELECT c.id, c.project_id, c.name, COALESCE(ca.appearance_type, 'icon'), COALESCE(ca.appearance_value, 'default')
		FROM collections c
		LEFT JOIN collection_appearances ca ON ca.collection_id = c.id
		ORDER BY c.name`)
	if err != nil {
		return nil, fmt.Errorf("listing collections: %w", err)
	}
	defer rows.Close()

	var collections []models.Collection
	for rows.Next() {
		var collection models.Collection
		if err := rows.Scan(&collection.ID, &collection.ProjectID, &collection.Name, &collection.Appearance.AppearanceType, &collection.Appearance.AppearanceValue); err != nil {
			return nil, fmt.Errorf("scanning collection: %w", err)
		}
		collection.Appearance.CollectionID = collection.ID
		collections = append(collections, collection)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating collections: %w", err)
	}

	return collections, nil
}

// UpdateCollection updates an existing collection's core fields.
func (s *CollectionService) UpdateCollection(collection models.Collection) (models.Collection, error) {
	if collection.ID == 0 {
		return models.Collection{}, fmt.Errorf("collection id is required")
	}
	_, err := s.db.Exec(
		"UPDATE collections SET project_id = ?, name = ? WHERE id = ?",
		collection.ProjectID, collection.Name, collection.ID,
	)
	if err != nil {
		return models.Collection{}, fmt.Errorf("updating collection: %w", err)
	}
	return collection, nil
}

// UpdateCollectionAppearance updates or inserts the appearance row for a collection.
func (s *CollectionService) UpdateCollectionAppearance(collectionID int64, appearance models.CollectionAppearance) (models.CollectionAppearance, error) {
	if collectionID == 0 {
		return models.CollectionAppearance{}, fmt.Errorf("collection id is required")
	}
	if appearance.AppearanceType != "icon" && appearance.AppearanceType != "color" {
		return models.CollectionAppearance{}, fmt.Errorf("appearance_type must be 'icon' or 'color'")
	}

	_, err := s.db.Exec(`
		INSERT INTO collection_appearances (collection_id, appearance_type, appearance_value)
		VALUES (?, ?, ?)
		ON CONFLICT(collection_id)
		DO UPDATE SET appearance_type = excluded.appearance_type, appearance_value = excluded.appearance_value`,
		collectionID, appearance.AppearanceType, appearance.AppearanceValue,
	)
	if err != nil {
		return models.CollectionAppearance{}, fmt.Errorf("updating collection appearance: %w", err)
	}

	appearance.CollectionID = collectionID
	return appearance, nil
}

// DeleteCollection removes a collection by ID. The database cascades the
// deletion to all requests in the collection, their responses, any favourite
// items referencing those requests, and the collection's appearance row.
// The IDs of the deleted requests are returned so the frontend can clean up
// its runtime selection state.
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
