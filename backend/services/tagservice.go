package services

import (
	"database/sql"
	"fmt"
	"strings"

	"snap-rq/backend/models"
)

// TagService manages tags and their many-to-many relationship with requests.
type TagService struct {
	db *sql.DB
}

// NewTagService returns a TagService backed by the given database.
func NewTagService(db *sql.DB) *TagService {
	return &TagService{db: db}
}

// GetAllTags returns every tag in the database, ordered by name.
func (s *TagService) GetAllTags() ([]models.Tag, error) {
	rows, err := s.db.Query("SELECT id, name FROM tags ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("listing tags: %w", err)
	}
	defer rows.Close()

	var tags []models.Tag
	for rows.Next() {
		var tag models.Tag
		if err := rows.Scan(&tag.ID, &tag.Name); err != nil {
			return nil, fmt.Errorf("scanning tag: %w", err)
		}
		tags = append(tags, tag)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating tags: %w", err)
	}

	return tags, nil
}

// GetTagsForRequest returns the tags attached to a single request.
func (s *TagService) GetTagsForRequest(requestID int64) ([]models.Tag, error) {
	rows, err := s.db.Query(`
		SELECT t.id, t.name
		FROM tags t
		JOIN request_tags rt ON rt.tag_id = t.id
		WHERE rt.request_id = ?
		ORDER BY t.name`,
		requestID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing tags for request: %w", err)
	}
	defer rows.Close()

	var tags []models.Tag
	for rows.Next() {
		var tag models.Tag
		if err := rows.Scan(&tag.ID, &tag.Name); err != nil {
			return nil, fmt.Errorf("scanning request tag: %w", err)
		}
		tags = append(tags, tag)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating request tags: %w", err)
	}

	return tags, nil
}

// GetTagsForRequests returns a map of request ID to tag names for a batch of requests.
func (s *TagService) GetTagsForRequests(requestIDs []int64) (map[int64][]string, error) {
	result := make(map[int64][]string)
	if len(requestIDs) == 0 {
		return result, nil
	}

	placeholders := make([]string, len(requestIDs))
	args := make([]interface{}, len(requestIDs))
	for i, id := range requestIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT rt.request_id, t.name
		FROM tags t
		JOIN request_tags rt ON rt.tag_id = t.id
		WHERE rt.request_id IN (%s)
		ORDER BY t.name`, strings.Join(placeholders, ", "))

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing tags for requests: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var requestID int64
		var name string
		if err := rows.Scan(&requestID, &name); err != nil {
			return nil, fmt.Errorf("scanning request tags batch: %w", err)
		}
		result[requestID] = append(result[requestID], name)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating request tags batch: %w", err)
	}

	return result, nil
}

// AddTagToRequest normalises the tag name, creates the tag if needed, and links it to the request.
func (s *TagService) AddTagToRequest(requestID int64, tagName string) (models.Tag, error) {
	name := normaliseTagName(tagName)
	if name == "" {
		return models.Tag{}, fmt.Errorf("tag name is required")
	}

	tag, err := s.findOrCreateTag(name)
	if err != nil {
		return models.Tag{}, err
	}

	_, err = s.db.Exec(
		"INSERT OR IGNORE INTO request_tags (request_id, tag_id) VALUES (?, ?)",
		requestID, tag.ID,
	)
	if err != nil {
		return models.Tag{}, fmt.Errorf("linking tag to request: %w", err)
	}

	return tag, nil
}

// RemoveTagFromRequest removes a tag from a single request.
func (s *TagService) RemoveTagFromRequest(requestID int64, tagName string) error {
	name := normaliseTagName(tagName)
	if name == "" {
		return fmt.Errorf("tag name is required")
	}

	var tagID int64
	err := s.db.QueryRow("SELECT id FROM tags WHERE name = ?", name).Scan(&tagID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return fmt.Errorf("finding tag: %w", err)
	}

	_, err = s.db.Exec(
		"DELETE FROM request_tags WHERE request_id = ? AND tag_id = ?",
		requestID, tagID,
	)
	if err != nil {
		return fmt.Errorf("unlinking tag from request: %w", err)
	}

	return nil
}

// SetRequestTags replaces all tags on a request with the given set.
func (s *TagService) SetRequestTags(requestID int64, tagNames []string) error {
	normalised := make([]string, 0, len(tagNames))
	seen := make(map[string]bool)
	for _, raw := range tagNames {
		name := normaliseTagName(raw)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		normalised = append(normalised, name)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec("DELETE FROM request_tags WHERE request_id = ?", requestID)
	if err != nil {
		return fmt.Errorf("clearing request tags: %w", err)
	}

	for _, name := range normalised {
		tag, err := s.findOrCreateTagTx(tx, name)
		if err != nil {
			return err
		}
		_, err = tx.Exec(
			"INSERT INTO request_tags (request_id, tag_id) VALUES (?, ?)",
			requestID, tag.ID,
		)
		if err != nil {
			return fmt.Errorf("linking tag to request: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}

	return nil
}

// DeleteTag removes a tag from the database, unlinking it from all requests.
func (s *TagService) DeleteTag(tagName string) error {
	name := normaliseTagName(tagName)
	if name == "" {
		return fmt.Errorf("tag name is required")
	}

	_, err := s.db.Exec("DELETE FROM tags WHERE name = ?", name)
	if err != nil {
		return fmt.Errorf("deleting tag: %w", err)
	}
	return nil
}

// GetRequestsForTag returns all requests that have the given tag.
func (s *TagService) GetRequestsForTag(tagName string) ([]models.HttpRequest, error) {
	name := normaliseTagName(tagName)
	rows, err := s.db.Query(`
		SELECT r.id, r.collection_id, r.name, r.url, r.method, r.body, r.request_headers, r.status_code, r.response_id
		FROM http_requests r
		JOIN request_tags rt ON rt.request_id = r.id
		JOIN tags t ON t.id = rt.tag_id
		WHERE t.name = ?
		ORDER BY r.name`,
		name,
	)
	if err != nil {
		return nil, fmt.Errorf("listing requests for tag: %w", err)
	}
	defer rows.Close()

	var requests []models.HttpRequest
	for rows.Next() {
		var req models.HttpRequest
		if err := rows.Scan(&req.ID, &req.CollectionID, &req.Name, &req.URL, &req.Method, &req.Body, &req.RequestHeaders, &req.StatusCode, &req.ResponseID); err != nil {
			return nil, fmt.Errorf("scanning request for tag: %w", err)
		}
		requests = append(requests, req)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating requests for tag: %w", err)
	}

	return requests, nil
}

func (s *TagService) findOrCreateTag(name string) (models.Tag, error) {
	var tag models.Tag
	err := s.db.QueryRow("SELECT id, name FROM tags WHERE name = ?", name).Scan(&tag.ID, &tag.Name)
	if err == nil {
		return tag, nil
	}
	if err != sql.ErrNoRows {
		return models.Tag{}, fmt.Errorf("finding tag: %w", err)
	}

	result, err := s.db.Exec("INSERT INTO tags (name) VALUES (?)", name)
	if err != nil {
		return models.Tag{}, fmt.Errorf("creating tag: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return models.Tag{}, fmt.Errorf("getting tag id: %w", err)
	}

	tag.ID = id
	tag.Name = name
	return tag, nil
}

func (s *TagService) findOrCreateTagTx(tx *sql.Tx, name string) (models.Tag, error) {
	var tag models.Tag
	err := tx.QueryRow("SELECT id, name FROM tags WHERE name = ?", name).Scan(&tag.ID, &tag.Name)
	if err == nil {
		return tag, nil
	}
	if err != sql.ErrNoRows {
		return models.Tag{}, fmt.Errorf("finding tag: %w", err)
	}

	result, err := tx.Exec("INSERT INTO tags (name) VALUES (?)", name)
	if err != nil {
		return models.Tag{}, fmt.Errorf("creating tag: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return models.Tag{}, fmt.Errorf("getting tag id: %w", err)
	}

	tag.ID = id
	tag.Name = name
	return tag, nil
}

func normaliseTagName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
