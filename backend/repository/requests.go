package repository

import (
	"database/sql"
	"errors"
	"fmt"

	"snap-rq/backend/models"
)

type Requests struct{ q Querier }

func NewRequests(q Querier) Requests { return Requests{q: q} }

// Get loads one request, with its project id joined in.
func (r Requests) Get(id int64) (models.HttpRequest, error) {
	var req models.HttpRequest
	err := r.q.QueryRow(`SELECT hr.id, hr.collection_id, c.project_id, hr.name, hr.url, hr.method,
		COALESCE(hr.body, ''), hr.body_type, COALESCE(hr.body_form, ''), COALESCE(hr.request_headers, ''),
		hr.status_code, hr.response_id, hr.user_sort_order, hr.user_color, hr.last_used_at
		FROM http_requests hr JOIN collections c ON c.id = hr.collection_id WHERE hr.id = ?`, id,
	).Scan(&req.ID, &req.CollectionID, &req.ProjectID, &req.Name, &req.URL, &req.Method,
		&req.Body, &req.BodyType, &req.BodyForm, &req.RequestHeaders,
		&req.StatusCode, &req.ResponseID, &req.UserSortOrder, &req.UserColor, &req.LastUsedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return models.HttpRequest{}, fmt.Errorf("request %d not found", id)
	}
	if err != nil {
		return models.HttpRequest{}, fmt.Errorf("loading request: %w", err)
	}
	return req, nil
}
