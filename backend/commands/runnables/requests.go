// Package runnables holds every command implementation, one file per
// domain. catalog.go assembles them into the list the engine registers.
package runnables

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
	"snap-rq/backend/store"
)

// ---------------------------------------------------------------------------
// Reads
// ---------------------------------------------------------------------------

// GetRequest returns one saved request.
type GetRequest struct{}

func (GetRequest) Info() commands.Info {
	return commands.Info{
		FunctionID: "request.get",
		Label:      "Get Request",
		ReadOnly:   true,
	}
}

func (GetRequest) Run(db store.Session, input json.RawMessage) (any, error) {
	var payload struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, err
	}
	return queryRequest(db, payload.ID)
}

// ListRequestSummaries returns every request as a lightweight summary.
type ListRequestSummaries struct{}

func (ListRequestSummaries) Info() commands.Info {
	return commands.Info{FunctionID: "request.list_summaries", Label: "List Requests", ReadOnly: true}
}

func (ListRequestSummaries) Run(db store.Session, _ json.RawMessage) (any, error) {
	return querySummaries(db, `ORDER BY hr.name`)
}

// ListRequestSummariesForCollection returns one collection's summaries in
// the user's custom order.
type ListRequestSummariesForCollection struct{}

func (ListRequestSummariesForCollection) Info() commands.Info {
	return commands.Info{FunctionID: "request.list_for_collection", Label: "List Collection Requests", ReadOnly: true}
}

func (ListRequestSummariesForCollection) Run(db store.Session, input json.RawMessage) (any, error) {
	var payload struct {
		CollectionID int64 `json:"collection_id"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, err
	}
	return querySummaries(db, `WHERE hr.collection_id = ? ORDER BY hr.user_sort_order, hr.name`, payload.CollectionID)
}

// ListRequestSummariesForProject returns every request of one project.
type ListRequestSummariesForProject struct{}

func (ListRequestSummariesForProject) Info() commands.Info {
	return commands.Info{FunctionID: "request.list_for_project", Label: "List Project Requests", ReadOnly: true}
}

func (ListRequestSummariesForProject) Run(db store.Session, input json.RawMessage) (any, error) {
	var payload struct {
		ProjectID int64 `json:"project_id"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, err
	}
	return querySummaries(db, `WHERE c.project_id = ? ORDER BY hr.name`, payload.ProjectID)
}

// ListRecentRequests returns the most recently used requests of a project.
type ListRecentRequests struct{}

func (ListRecentRequests) Info() commands.Info {
	return commands.Info{FunctionID: "request.list_recent", Label: "List Recent Requests", ReadOnly: true}
}

func (ListRecentRequests) Run(db store.Session, input json.RawMessage) (any, error) {
	var payload struct {
		ProjectID int64 `json:"project_id"`
		Limit     int   `json:"limit"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, err
	}
	if payload.Limit <= 0 {
		payload.Limit = 50
	}
	if payload.Limit > 100 {
		payload.Limit = 100
	}
	return querySummaries(db,
		`JOIN request_usage ru ON ru.request_id = hr.id
		 WHERE c.project_id = ? ORDER BY ru.last_used_at DESC, hr.name ASC LIMIT ?`,
		payload.ProjectID, payload.Limit)
}

// ListRequestSummariesByIDs returns the given requests' summaries; ids
// that no longer exist are skipped. Used by the frontend's targeted store
// sync after commands.
type ListRequestSummariesByIDs struct{}

func (ListRequestSummariesByIDs) Info() commands.Info {
	return commands.Info{FunctionID: "request.list_by_ids", Label: "List Requests By Ids", ReadOnly: true}
}

func (ListRequestSummariesByIDs) Run(db store.Session, input json.RawMessage) (any, error) {
	var payload struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, err
	}
	if len(payload.IDs) == 0 {
		return []models.HttpRequestSummary{}, nil
	}
	return querySummaries(db,
		fmt.Sprintf(`WHERE hr.id IN (%s)`, placeholders(len(payload.IDs))),
		int64Args(payload.IDs)...)
}

// ---------------------------------------------------------------------------
// Writes
// ---------------------------------------------------------------------------

// CreateRequest saves a new request, appended to the end of its
// collection's order. Undo removes it.
type CreateRequest struct{}

func (CreateRequest) Info() commands.Info {
	return commands.Info{
		FunctionID:      "request.create",
		Label:           "Create Request",
		UndoOperationID: "request.delete",
	}
}

func (CreateRequest) Run(db store.Session, input json.RawMessage) (any, error) {
	var req models.HttpRequest
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	if req.CollectionID == 0 {
		return nil, errors.New("collection id is required")
	}

	nextOrder, err := nextSortOrder(db, req.CollectionID)
	if err != nil {
		return nil, err
	}
	id, err := db.Insert("http_requests", map[string]any{
		"collection_id":   req.CollectionID,
		"name":            req.Name,
		"url":             req.URL,
		"method":          req.Method,
		"body":            req.Body,
		"body_type":       normalizeBodyType(req.BodyType),
		"request_headers": req.RequestHeaders,
		"user_sort_order": nextOrder,
	})
	if err != nil {
		return nil, err
	}
	return queryRequest(db, id)
}

// UpdateRequest changes the user's own columns of a request. The send
// flow's columns (status_code, response_id) are never written here: undo
// must not be able to wipe out the result of a request that was sent.
type UpdateRequest struct{}

func (UpdateRequest) Info() commands.Info {
	return commands.Info{
		FunctionID:      "request.update",
		Label:           "Update Request",
		UndoOperationID: "request.update",
		Columns: []string{
			"collection_id", "name", "url", "method", "body", "body_type",
			"request_headers", "user_sort_order",
		},
	}
}

func (UpdateRequest) Run(db store.Session, input json.RawMessage) (any, error) {
	var req models.HttpRequest
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	if req.ID == 0 {
		return nil, errors.New("request id is required")
	}
	if req.CollectionID == 0 {
		return nil, errors.New("collection id is required")
	}

	// A move to another collection appends the request to the target
	// collection's order; otherwise its position is kept.
	columns := map[string]any{
		"collection_id":   req.CollectionID,
		"name":            req.Name,
		"url":             req.URL,
		"method":          req.Method,
		"body":            req.Body,
		"body_type":       normalizeBodyType(req.BodyType),
		"request_headers": req.RequestHeaders,
	}
	var currentCollection int64
	if err := db.QueryRow(
		`SELECT collection_id FROM http_requests WHERE id = ?`, req.ID,
	).Scan(&currentCollection); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("request %d not found", req.ID)
		}
		return nil, fmt.Errorf("loading request: %w", err)
	}
	if currentCollection != req.CollectionID {
		nextOrder, err := nextSortOrder(db, req.CollectionID)
		if err != nil {
			return nil, err
		}
		columns["user_sort_order"] = nextOrder
	}

	if err := db.Update("http_requests", req.ID, columns); err != nil {
		return nil, err
	}
	return queryRequest(db, req.ID)
}

// DeleteRequests bins the given requests: a trash row is written for each
// while it still exists, then one delete removes them (the cascades take
// their responses, tag and favourite memberships with them — saved
// first, so undo puts the whole tree back). One undo step for the batch.
type DeleteRequests struct{}

func (DeleteRequests) Info() commands.Info {
	return commands.Info{
		FunctionID:      "request.delete",
		Label:           "Delete Requests",
		UndoOperationID: "bin.restore",
	}
}

func (DeleteRequests) Run(db store.Session, input json.RawMessage) (any, error) {
	var payload struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, err
	}
	if len(payload.IDs) == 0 {
		return nil, nil
	}

	for _, id := range payload.IDs {
		if err := writeBinRow(db, id); err != nil {
			return nil, err
		}
	}
	return nil, db.Delete("http_requests", payload.IDs...)
}

// BulkSetRequestField writes one field onto many requests at once. Undo
// restores every row's previous value in one step.
type BulkSetRequestField struct{}

// bulkFields maps the payload's field name to the column it writes. The
// names are fixed vocabulary, never taken from input directly.
var bulkFields = map[string]string{
	"name":      "name",
	"url":       "url",
	"method":    "method",
	"headers":   "request_headers",
	"body":      "body",
	"body_type": "body_type",
	"color":     "user_color",
}

func (BulkSetRequestField) Info() commands.Info {
	return commands.Info{
		FunctionID:      "request.bulk_set",
		Label:           "Edit Requests",
		UndoOperationID: "request.bulk_set",
		Columns:         []string{"name", "url", "method", "request_headers", "body", "body_type", "user_color"},
	}
}

func (BulkSetRequestField) Run(db store.Session, input json.RawMessage) (any, error) {
	var payload struct {
		IDs   []int64 `json:"ids"`
		Field string  `json:"field"`
		Value any     `json:"value"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, err
	}
	column, known := bulkFields[payload.Field]
	if !known {
		return nil, fmt.Errorf("unknown field %q", payload.Field)
	}
	if len(payload.IDs) == 0 {
		return nil, nil
	}

	for _, id := range payload.IDs {
		values := map[string]any{column: payload.Value}
		if column == "body" {
			values["body_type"] = normalizeBodyType(stringValue(payload.Value))
		}
		if err := db.Update("http_requests", id, values); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

// DuplicateRequest copies a request together with its tags and favourite
// memberships. Undo removes the copy.
type DuplicateRequest struct{}

func (DuplicateRequest) Info() commands.Info {
	return commands.Info{
		FunctionID:      "request.duplicate",
		Label:           "Duplicate Request",
		UndoOperationID: "request.delete",
	}
}

func (DuplicateRequest) Run(db store.Session, input json.RawMessage) (any, error) {
	var payload struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, err
	}
	if payload.ID == 0 {
		return nil, errors.New("request id is required")
	}

	original, err := queryRequest(db, payload.ID)
	if err != nil {
		return nil, err
	}

	nextOrder, err := nextSortOrder(db, original.CollectionID)
	if err != nil {
		return nil, err
	}
	newID, err := db.Insert("http_requests", map[string]any{
		"collection_id":   original.CollectionID,
		"name":            original.Name + " (copy)",
		"url":             original.URL,
		"method":          original.Method,
		"body":            original.Body,
		"body_type":       original.BodyType,
		"request_headers": original.RequestHeaders,
		"user_sort_order": nextOrder,
		"user_color":      original.UserColor,
	})
	if err != nil {
		return nil, err
	}

	// Copy tag and favourite memberships as new rows, so undo removes them
	// with the copy.
	if err := copyMemberships(db, newID, payload.ID); err != nil {
		return nil, err
	}
	return queryRequest(db, newID)
}

// copyMemberships duplicates the original's tag and favourite memberships
// for the copy, as observed inserts so undo removes them with the copy.
func copyMemberships(db store.Session, newID, originalID int64) error {
	if err := copyTagMemberships(db, newID, originalID); err != nil {
		return err
	}
	return copyFavouriteMemberships(db, newID, originalID)
}

func copyTagMemberships(db store.Session, newID, originalID int64) error {
	rows, err := db.Query(
		`SELECT tag_id, user_color FROM request_tags WHERE request_id = ?`, originalID)
	if err != nil {
		return fmt.Errorf("listing tag memberships: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var tagID int64
		var color sql.NullString
		if err := rows.Scan(&tagID, &color); err != nil {
			return fmt.Errorf("reading a tag membership: %w", err)
		}
		values := map[string]any{"request_id": newID, "tag_id": tagID}
		if color.Valid {
			values["user_color"] = color.String
		}
		if _, err := db.Insert("request_tags", values); err != nil {
			return err
		}
	}
	return rows.Err()
}

func copyFavouriteMemberships(db store.Session, newID, originalID int64) error {
	rows, err := db.Query(
		`SELECT favourite_collection_id, user_color FROM favourite_items WHERE http_request_id = ?`, originalID)
	if err != nil {
		return fmt.Errorf("listing favourite memberships: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var collectionID int64
		var color sql.NullString
		if err := rows.Scan(&collectionID, &color); err != nil {
			return fmt.Errorf("reading a favourite membership: %w", err)
		}
		values := map[string]any{"http_request_id": newID, "favourite_collection_id": collectionID}
		if color.Valid {
			values["user_color"] = color.String
		}
		if _, err := db.Insert("favourite_items", values); err != nil {
			return err
		}
	}
	return rows.Err()
}

// UpdateRequestOrder persists a custom order for one collection as one
// bulk statement. Its undo is equally one bulk statement (the Undoer
// interface), restoring the previous order from the captured values.
type UpdateRequestOrder struct{}

func (UpdateRequestOrder) Info() commands.Info {
	return commands.Info{
		FunctionID:      "request.update_order",
		Label:           "Reorder Requests",
		UndoOperationID: "request.update_order",
		Columns:         []string{"user_sort_order"},
	}
}

// CaptureRows tells the engine to save the current values of the rows the bulk statement
// will touch, reading the declared columns (the old order values).
func (UpdateRequestOrder) CaptureRows(input json.RawMessage) map[string][]int64 {
	var payload struct {
		OrderedIDs []int64 `json:"ordered_ids"`
	}
	if json.Unmarshal(input, &payload) != nil {
		return nil
	}
	return map[string][]int64{"http_requests": payload.OrderedIDs}
}

func (UpdateRequestOrder) Run(db store.Session, input json.RawMessage) (any, error) {
	var payload struct {
		CollectionID int64   `json:"collection_id"`
		OrderedIDs   []int64 `json:"ordered_ids"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, err
	}
	if payload.CollectionID == 0 {
		return nil, errors.New("collection id is required")
	}
	if len(payload.OrderedIDs) == 0 {
		return nil, nil
	}

	query := `UPDATE http_requests SET user_sort_order = CASE id`
	args := make([]any, 0, len(payload.OrderedIDs)*2+1)
	for index, id := range payload.OrderedIDs {
		query += " WHEN ? THEN ?"
		args = append(args, id, index)
	}
	query += " ELSE user_sort_order END WHERE collection_id = ?"
	args = append(args, payload.CollectionID)

	if _, err := db.Exec(query, args...); err != nil {
		return nil, fmt.Errorf("saving the new order: %w", err)
	}
	return nil, nil
}

// Undo restores the previous order with one symmetric bulk statement
// instead of one update per row.
func (UpdateRequestOrder) Undo(db store.Session, s *commands.Step) error {
	query := `UPDATE http_requests SET user_sort_order = CASE id`
	args := make([]any, 0, len(s.ColumnChanges)*2)
	for _, change := range s.ColumnChanges {
		order, _ := change.OldValues["user_sort_order"].(int64)
		query += " WHEN ? THEN ?"
		args = append(args, change.ID, order)
	}
	query += " ELSE user_sort_order END"

	if _, err := db.Exec(query, args...); err != nil {
		return fmt.Errorf("restoring the previous order: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Shared query helpers
// ---------------------------------------------------------------------------

const summaryColumns = `hr.id, hr.collection_id, c.project_id, hr.name, hr.url, hr.method, hr.status_code, hr.response_id, hr.user_sort_order, hr.user_color`

const requestColumns = `hr.id, hr.collection_id, c.project_id, hr.name, hr.url, hr.method,
	COALESCE(hr.body, ''), hr.body_type, COALESCE(hr.request_headers, ''),
	hr.status_code, hr.response_id, hr.user_sort_order, hr.user_color`

func queryRequest(db store.Session, id int64) (models.HttpRequest, error) {
	var req models.HttpRequest
	err := db.QueryRow(
		`SELECT `+requestColumns+` FROM http_requests hr
		 JOIN collections c ON c.id = hr.collection_id WHERE hr.id = ?`, id,
	).Scan(&req.ID, &req.CollectionID, &req.ProjectID, &req.Name, &req.URL, &req.Method,
		&req.Body, &req.BodyType, &req.RequestHeaders,
		&req.StatusCode, &req.ResponseID, &req.UserSortOrder, &req.UserColor)
	if err == sql.ErrNoRows {
		return models.HttpRequest{}, fmt.Errorf("request %d not found", id)
	}
	if err != nil {
		return models.HttpRequest{}, fmt.Errorf("loading request: %w", err)
	}
	return req, nil
}

func querySummaries(db store.Session, suffix string, args ...any) ([]models.HttpRequestSummary, error) {
	rows, err := db.Query(
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
			&s.StatusCode, &s.ResponseID, &s.UserSortOrder, &s.UserColor); err != nil {
			return nil, fmt.Errorf("reading a request summary: %w", err)
		}
		summaries = append(summaries, s)
	}
	return summaries, rows.Err()
}

func nextSortOrder(db store.Session, collectionID int64) (int, error) {
	var next int
	if err := db.QueryRow(
		`SELECT COALESCE(MAX(user_sort_order), -1) + 1 FROM http_requests WHERE collection_id = ?`,
		collectionID,
	).Scan(&next); err != nil {
		return 0, fmt.Errorf("computing the next order: %w", err)
	}
	return next, nil
}

// normalizeBodyType guards against empty/invalid values so the body_type
// column always stores one of the allowed values.
func normalizeBodyType(bodyType string) string {
	if bodyType == "text" {
		return "text"
	}
	return "json"
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func placeholders(n int) string {
	out := ""
	for i := 0; i < n; i++ {
		if i > 0 {
			out += ","
		}
		out += "?"
	}
	return out
}

func int64Args(ids []int64) []any {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return args
}
