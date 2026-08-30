package services

import (
	"database/sql"
	"fmt"

	"snap-rq/backend/models"
)

// FavouriteService provides CRUD operations for favourite collections and the
// items that link HTTP requests into them.
type FavouriteService struct {
	db *sql.DB
}

// NewFavouriteService returns a FavouriteService backed by the given database.
func NewFavouriteService(db *sql.DB) *FavouriteService {
	return &FavouriteService{db: db}
}

// CreateFavouriteCollection saves a new favourite collection.
func (s *FavouriteService) CreateFavouriteCollection(collection models.FavouriteCollection) (models.FavouriteCollection, error) {
	if collection.ProfileID == 0 {
		return models.FavouriteCollection{}, fmt.Errorf("profile id is required")
	}

	result, err := s.db.Exec(
		"INSERT INTO favourite_collections (profile_id, name) VALUES (?, ?)",
		collection.ProfileID, collection.Name,
	)
	if err != nil {
		return models.FavouriteCollection{}, fmt.Errorf("creating favourite collection: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return models.FavouriteCollection{}, fmt.Errorf("getting last insert id: %w", err)
	}

	collection.ID = id
	return collection, nil
}

// GetFavouriteCollection retrieves a single favourite collection by ID.
func (s *FavouriteService) GetFavouriteCollection(id int64) (models.FavouriteCollection, error) {
	var collection models.FavouriteCollection
	row := s.db.QueryRow(
		"SELECT id, profile_id, name, created_at FROM favourite_collections WHERE id = ?",
		id,
	)
	err := row.Scan(&collection.ID, &collection.ProfileID, &collection.Name, &collection.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return models.FavouriteCollection{}, fmt.Errorf("favourite collection not found")
		}
		return models.FavouriteCollection{}, fmt.Errorf("getting favourite collection: %w", err)
	}
	return collection, nil
}

// GetFavouriteCollectionsForProfile returns all favourite collections for a profile, newest first.
func (s *FavouriteService) GetFavouriteCollectionsForProfile(profileID int64) ([]models.FavouriteCollection, error) {
	rows, err := s.db.Query(
		"SELECT id, profile_id, name, created_at FROM favourite_collections WHERE profile_id = ? ORDER BY created_at DESC",
		profileID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing favourite collections: %w", err)
	}
	defer rows.Close()

	var collections []models.FavouriteCollection
	for rows.Next() {
		var collection models.FavouriteCollection
		if err := rows.Scan(&collection.ID, &collection.ProfileID, &collection.Name, &collection.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning favourite collection: %w", err)
		}
		collections = append(collections, collection)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating favourite collections: %w", err)
	}

	return collections, nil
}

// UpdateFavouriteCollection updates an existing favourite collection.
func (s *FavouriteService) UpdateFavouriteCollection(collection models.FavouriteCollection) (models.FavouriteCollection, error) {
	if collection.ID == 0 {
		return models.FavouriteCollection{}, fmt.Errorf("favourite collection id is required")
	}

	_, err := s.db.Exec(
		"UPDATE favourite_collections SET name = ? WHERE id = ?",
		collection.Name, collection.ID,
	)
	if err != nil {
		return models.FavouriteCollection{}, fmt.Errorf("updating favourite collection: %w", err)
	}

	return collection, nil
}

// DeleteFavouriteCollection removes a favourite collection and its items.
// The actual HTTP requests are not deleted.
func (s *FavouriteService) DeleteFavouriteCollection(id int64) error {
	_, err := s.db.Exec("DELETE FROM favourite_collections WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("deleting favourite collection: %w", err)
	}
	return nil
}

// AddRequestToFavouriteCollection adds a request to a favourite collection.
func (s *FavouriteService) AddRequestToFavouriteCollection(favouriteCollectionID, httpRequestID int64) (models.FavouriteItem, error) {
	var nextSort int
	if err := s.db.QueryRow(
		"SELECT COALESCE(MAX(sort_order), 0) + 1 FROM favourite_items WHERE favourite_collection_id = ?",
		favouriteCollectionID,
	).Scan(&nextSort); err != nil {
		return models.FavouriteItem{}, fmt.Errorf("calculating sort order: %w", err)
	}

	result, err := s.db.Exec(
		`INSERT INTO favourite_items (favourite_collection_id, http_request_id, sort_order)
		 VALUES (?, ?, ?)`,
		favouriteCollectionID, httpRequestID, nextSort,
	)
	if err != nil {
		return models.FavouriteItem{}, fmt.Errorf("adding favourite item: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return models.FavouriteItem{}, fmt.Errorf("getting last insert id: %w", err)
	}

	return models.FavouriteItem{
		ID:                    id,
		FavouriteCollectionID: favouriteCollectionID,
		HTTPRequestID:         httpRequestID,
		SortOrder:             nextSort,
	}, nil
}

// RemoveRequestFromFavouriteCollection removes a request from a favourite collection.
func (s *FavouriteService) RemoveRequestFromFavouriteCollection(favouriteCollectionID, httpRequestID int64) error {
	_, err := s.db.Exec(
		"DELETE FROM favourite_items WHERE favourite_collection_id = ? AND http_request_id = ?",
		favouriteCollectionID, httpRequestID,
	)
	if err != nil {
		return fmt.Errorf("removing favourite item: %w", err)
	}
	return nil
}

// GetRequestsForFavouriteCollection returns all HTTP requests in a favourite collection.
func (s *FavouriteService) GetRequestsForFavouriteCollection(favouriteCollectionID int64) ([]models.HttpRequest, error) {
	rows, err := s.db.Query(
		`SELECT r.id, r.collection_id, r.name, r.url, r.method, r.body, r.request_headers, r.status_code, r.response_id
		 FROM http_requests r
		 JOIN favourite_items fi ON fi.http_request_id = r.id
		 WHERE fi.favourite_collection_id = ?
		 ORDER BY fi.sort_order, r.name`,
		favouriteCollectionID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing favourite requests: %w", err)
	}
	defer rows.Close()

	var requests []models.HttpRequest
	for rows.Next() {
		var req models.HttpRequest
		if err := rows.Scan(
			&req.ID, &req.CollectionID, &req.Name, &req.URL, &req.Method,
			&req.Body, &req.RequestHeaders, &req.StatusCode, &req.ResponseID,
		); err != nil {
			return nil, fmt.Errorf("scanning request: %w", err)
		}
		requests = append(requests, req)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating favourite requests: %w", err)
	}

	return requests, nil
}

// GetFavouriteCollectionIDsForRequest returns the IDs of all favourite collections
// that contain the given HTTP request. This drives the heart popup checkmarks.
func (s *FavouriteService) GetFavouriteCollectionIDsForRequest(httpRequestID int64) ([]int64, error) {
	rows, err := s.db.Query(
		"SELECT favourite_collection_id FROM favourite_items WHERE http_request_id = ?",
		httpRequestID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing favourite collection ids: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scanning favourite collection id: %w", err)
		}
		ids = append(ids, id)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating favourite collection ids: %w", err)
	}

	return ids, nil
}
