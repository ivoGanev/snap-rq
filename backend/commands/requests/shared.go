// Package requests holds the request commands, one operation per file.
// shared.go holds only the helpers several files here use.
package requests

import (
	"database/sql"
	"fmt"

	"snap-rq/backend/models"
)

// requestColumns selects one full request row, with NULL-safe text columns.
const requestColumns = `hr.id, hr.collection_id, c.project_id, hr.name, hr.url, hr.method,
	COALESCE(hr.body, ''), hr.body_type, COALESCE(hr.body_form, ''), COALESCE(hr.request_headers, ''),
	hr.status_code, hr.response_id, hr.user_sort_order, hr.user_color, hr.last_used_at`

// queryRequest loads one request, joining its project id in.
func queryRequest(tx *sql.Tx, id int64) (models.HttpRequest, error) {
	var req models.HttpRequest
	err := tx.QueryRow(
		`SELECT `+requestColumns+` FROM http_requests hr
		 JOIN collections c ON c.id = hr.collection_id WHERE hr.id = ?`, id,
	).Scan(&req.ID, &req.CollectionID, &req.ProjectID, &req.Name, &req.URL, &req.Method,
		&req.Body, &req.BodyType, &req.BodyForm, &req.RequestHeaders,
		&req.StatusCode, &req.ResponseID, &req.UserSortOrder, &req.UserColor, &req.LastUsedAt)
	if err == sql.ErrNoRows {
		return models.HttpRequest{}, fmt.Errorf("request %d not found", id)
	}
	if err != nil {
		return models.HttpRequest{}, fmt.Errorf("loading request: %w", err)
	}
	return req, nil
}

// summaryColumns selects the lightweight projection the list views use.
const summaryColumns = `hr.id, hr.collection_id, c.project_id, hr.name, hr.url, hr.method,
	hr.status_code, hr.response_id, hr.user_sort_order, hr.user_color, hr.last_used_at`

// querySummaries runs a summary query with the standard FROM/JOIN prefix.
func querySummaries(tx *sql.Tx, suffix string, args ...any) ([]models.HttpRequestSummary, error) {
	rows, err := tx.Query(
		`SELECT `+summaryColumns+` FROM http_requests hr JOIN collections c ON c.id = hr.collection_id `+suffix,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("listing requests: %w", err)
	}
	defer rows.Close()

	var summaries []models.HttpRequestSummary
	for rows.Next() {
		var s models.HttpRequestSummary
		if err := rows.Scan(&s.ID, &s.CollectionID, &s.ProjectID, &s.Name, &s.URL, &s.Method,
			&s.StatusCode, &s.ResponseID, &s.UserSortOrder, &s.UserColor, &s.LastUsedAt); err != nil {
			return nil, fmt.Errorf("reading a request summary: %w", err)
		}
		summaries = append(summaries, s)
	}
	return summaries, rows.Err()
}

// nextSortOrder computes the position a new or moved request gets at the
// end of a collection's custom order.
func nextSortOrder(tx *sql.Tx, collectionID int64) (int, error) {
	var next int
	if err := tx.QueryRow(
		`SELECT COALESCE(MAX(user_sort_order), -1) + 1 FROM http_requests WHERE collection_id = ?`,
		collectionID,
	).Scan(&next); err != nil {
		return 0, fmt.Errorf("computing the next order: %w", err)
	}
	return next, nil
}

// normalizeBodyType guards against empty/invalid values.
func normalizeBodyType(bodyType string) string {
	if bodyType == "text" {
		return "text"
	}
	return "json"
}
