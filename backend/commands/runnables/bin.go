package runnables

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
	"snap-rq/backend/store"
)

// The trash bin. Deleting a request from a view is "move to bin"; undo of
// that delete removes the bin row and restores the request tree. bin.restore
// is the user-facing trash button: it recreates requests (new ids) from
// the bin snapshots — and is itself undoable (undo deletes what it made
// and puts the bin rows back).

// ListBinnedRequests returns a project's binned requests, newest first.
type ListBinnedRequests struct{}

func (ListBinnedRequests) Info() commands.Info {
	return commands.Info{FunctionID: "bin.list", Label: "List Binned Requests", ReadOnly: true}
}

func (ListBinnedRequests) Run(db store.Session, input json.RawMessage) (any, error) {
	var payload struct {
		ProjectID int64 `json:"project_id"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, err
	}
	rows, err := db.Query(`
		SELECT id, original_request_id, project_id, collection_id, original_collection_id,
		       original_collection_name, name, url, method, status_code, response_id, deleted_at
		FROM binned_requests WHERE project_id = ?
		ORDER BY deleted_at DESC, id DESC`, payload.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("listing binned requests: %w", err)
	}
	defer rows.Close()

	var summaries []models.HttpBinnedRequestSummary
	for rows.Next() {
		var s models.HttpBinnedRequestSummary
		if err := rows.Scan(&s.ID, &s.OriginalRequestID, &s.ProjectID, &s.CollectionID,
			&s.OriginalCollectionID, &s.OriginalCollectionName,
			&s.Name, &s.URL, &s.Method, &s.StatusCode, &s.ResponseID, &s.DeletedAt); err != nil {
			return nil, fmt.Errorf("reading a binned request: %w", err)
		}
		summaries = append(summaries, s)
	}
	return summaries, rows.Err()
}

// writeBinRow saves a copy of the request into the trash while it still exists.
// The insert is observed, so undo of the delete removes the bin row too.
func writeBinRow(db store.Session, requestID int64) error {
	req, err := queryRequest(db, requestID)
	if err != nil {
		return err
	}

	snapshot, err := buildSnapshot(db, requestID)
	if err != nil {
		return err
	}
	snapshotJSON, err := snapshot.MarshalJSON()
	if err != nil {
		return fmt.Errorf("serialising the snapshot: %w", err)
	}

	var collectionName string
	if err := db.QueryRow(
		`SELECT name FROM collections WHERE id = ?`, req.CollectionID,
	).Scan(&collectionName); err != nil {
		collectionName = ""
	}

	_, err = db.Insert("binned_requests", map[string]any{
		"original_request_id":      req.ID,
		"project_id":               req.ProjectID,
		"collection_id":            req.CollectionID,
		"original_collection_id":   req.CollectionID,
		"original_collection_name": collectionName,
		"name":                     req.Name,
		"url":                      req.URL,
		"method":                   req.Method,
		"body":                     req.Body,
		"body_type":                req.BodyType,
		"request_headers":          req.RequestHeaders,
		"status_code":              req.StatusCode,
		"response_id":              0,
		"snapshot_json":            string(snapshotJSON),
	})
	return err
}

func buildSnapshot(db store.Session, requestID int64) (models.BinnedRequestSnapshot, error) {
	snapshot := models.BinnedRequestSnapshot{}

	rows, err := db.Query(`
		SELECT id, COALESCE(headers, ''), status_code, COALESCE(body, ''), created_at, duration_ms
		FROM responses WHERE request_id = ? ORDER BY id`, requestID)
	if err != nil {
		return snapshot, fmt.Errorf("loading responses: %w", err)
	}
	for rows.Next() {
		var resp models.BinnedResponseSnapshot
		if err := rows.Scan(&resp.ID, &resp.Headers, &resp.StatusCode, &resp.Body,
			&resp.CreatedAt, &resp.DurationMs); err != nil {
			rows.Close()
			return snapshot, fmt.Errorf("reading a response: %w", err)
		}
		snapshot.Responses = append(snapshot.Responses, resp)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return snapshot, err
	}

	rows, err = db.Query(`
		SELECT t.id, t.name FROM request_tags rt
		JOIN tags t ON t.id = rt.tag_id WHERE rt.request_id = ? ORDER BY t.name`, requestID)
	if err != nil {
		return snapshot, fmt.Errorf("loading tags: %w", err)
	}
	for rows.Next() {
		var tag models.BinnedTagSnapshot
		if err := rows.Scan(&tag.ID, &tag.Name); err != nil {
			rows.Close()
			return snapshot, fmt.Errorf("reading a tag: %w", err)
		}
		snapshot.Tags = append(snapshot.Tags, tag)
	}
	rows.Close()

	rows, err = db.Query(`
		SELECT fc.id, fc.name FROM favourite_items fi
		JOIN favourite_collections fc ON fc.id = fi.favourite_collection_id
		WHERE fi.http_request_id = ? ORDER BY fc.name`, requestID)
	if err != nil {
		return snapshot, fmt.Errorf("loading favourites: %w", err)
	}
	for rows.Next() {
		var fav models.BinnedFavouriteSnapshot
		if err := rows.Scan(&fav.ID, &fav.Name); err != nil {
			rows.Close()
			return snapshot, fmt.Errorf("reading a favourite: %w", err)
		}
		snapshot.Favourites = append(snapshot.Favourites, fav)
	}
	rows.Close()

	return snapshot, nil
}

// RestoreBinnedRequests recreates binned requests into the given
// collections. It is undoable as a whole: undo deletes every recreated
// row and puts the bin rows back.
type RestoreBinnedRequests struct{}

func (RestoreBinnedRequests) Info() commands.Info {
	return commands.Info{
		FunctionID:      "bin.restore",
		Label:           "Restore Requests",
		UndoOperationID: "request.delete",
		// The newest-response link is written while recreating history.
		Columns: []string{"response_id"},
	}
}

func (RestoreBinnedRequests) Run(db store.Session, input json.RawMessage) (any, error) {
	var payload struct {
		Items []struct {
			BinID        int64 `json:"bin_id"`
			CollectionID int64 `json:"collection_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, err
	}

	restored := make([]models.HttpRequest, 0, len(payload.Items))
	for _, item := range payload.Items {
		req, err := restoreOne(db, item.BinID, item.CollectionID)
		if err != nil {
			return nil, fmt.Errorf("restoring binned request %d: %w", item.BinID, err)
		}
		restored = append(restored, req)
	}
	return restored, nil
}

func restoreOne(db store.Session, binID, targetCollectionID int64) (models.HttpRequest, error) {
	var targetProjectID int64
	if err := db.QueryRow(
		`SELECT project_id FROM collections WHERE id = ?`, targetCollectionID,
	).Scan(&targetProjectID); err != nil {
		if err == sql.ErrNoRows {
			return models.HttpRequest{}, fmt.Errorf("target collection not found")
		}
		return models.HttpRequest{}, fmt.Errorf("loading target collection: %w", err)
	}

	var binned struct {
		ProjectID      int64
		Name           string
		URL            string
		Method         string
		Body           string
		BodyType       string
		RequestHeaders string
		StatusCode     int
		ResponseID     int64
		SnapshotJSON   string
	}
	if err := db.QueryRow(`
		SELECT project_id, name, url, method, COALESCE(body, ''), body_type,
		       COALESCE(request_headers, ''), status_code, response_id, snapshot_json
		FROM binned_requests WHERE id = ?`, binID,
	).Scan(&binned.ProjectID, &binned.Name, &binned.URL, &binned.Method,
		&binned.Body, &binned.BodyType, &binned.RequestHeaders,
		&binned.StatusCode, &binned.ResponseID, &binned.SnapshotJSON); err != nil {
		if err == sql.ErrNoRows {
			return models.HttpRequest{}, fmt.Errorf("binned request not found")
		}
		return models.HttpRequest{}, fmt.Errorf("loading binned request: %w", err)
	}

	if binned.ProjectID != targetProjectID {
		return models.HttpRequest{}, fmt.Errorf("target collection does not belong to the binned request's project")
	}

	snapshot, err := models.UnmarshalBinnedRequestSnapshot(binned.SnapshotJSON)
	if err != nil {
		return models.HttpRequest{}, fmt.Errorf("parsing the snapshot: %w", err)
	}

	nextOrder, err := nextSortOrder(db, targetCollectionID)
	if err != nil {
		return models.HttpRequest{}, err
	}
	newID, err := db.Insert("http_requests", map[string]any{
		"collection_id":   targetCollectionID,
		"name":            binned.Name,
		"url":             binned.URL,
		"method":          binned.Method,
		"body":            binned.Body,
		"body_type":       normalizeBodyType(binned.BodyType),
		"request_headers": binned.RequestHeaders,
		"status_code":     binned.StatusCode,
		"response_id":     0,
		"user_sort_order": nextOrder,
	})
	if err != nil {
		return models.HttpRequest{}, err
	}

	// Recreate the response history; map old snapshot ids to the new rows
	// so the newest-response link stays correct.
	newestResponseID := int64(0)
	for _, resp := range snapshot.Responses {
		id, err := db.Insert("responses", map[string]any{
			"request_id":  newID,
			"headers":     resp.Headers,
			"status_code": resp.StatusCode,
			"body":        resp.Body,
			"created_at":  resp.CreatedAt,
			"duration_ms": resp.DurationMs,
		})
		if err != nil {
			return models.HttpRequest{}, err
		}
		if resp.ID == binned.ResponseID {
			newestResponseID = id
		}
	}
	if newestResponseID != 0 {
		if err := db.Update("http_requests", newID, map[string]any{"response_id": newestResponseID}); err != nil {
			return models.HttpRequest{}, err
		}
	}

	// Re-attach tags and favourites that still exist.
	for _, tag := range snapshot.Tags {
		var tagID int64
		err := db.QueryRow(
			`SELECT id FROM tags WHERE id = ? AND project_id = ?`, tag.ID, targetProjectID,
		).Scan(&tagID)
		if err == sql.ErrNoRows {
			err = db.QueryRow(
				`SELECT id FROM tags WHERE name = ? AND project_id = ?`, tag.Name, targetProjectID,
			).Scan(&tagID)
		}
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return models.HttpRequest{}, fmt.Errorf("looking up tag: %w", err)
		}
		if _, err := db.Insert("request_tags", map[string]any{
			"request_id": newID, "tag_id": tagID,
		}); err != nil {
			return models.HttpRequest{}, err
		}
	}

	for _, fav := range snapshot.Favourites {
		var favID int64
		err := db.QueryRow(
			`SELECT id FROM favourite_collections WHERE id = ? AND project_id = ?`, fav.ID, targetProjectID,
		).Scan(&favID)
		if err == sql.ErrNoRows {
			err = db.QueryRow(
				`SELECT id FROM favourite_collections WHERE name = ? AND project_id = ?`, fav.Name, targetProjectID,
			).Scan(&favID)
		}
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return models.HttpRequest{}, fmt.Errorf("looking up favourite: %w", err)
		}
		if _, err := db.Insert("favourite_items", map[string]any{
			"favourite_collection_id": favID, "http_request_id": newID,
		}); err != nil {
			return models.HttpRequest{}, err
		}
	}

	// The bin row's work is done; removing it is observed, so undo of the
	// restore puts it back.
	if err := db.Delete("binned_requests", binID); err != nil {
		return models.HttpRequest{}, err
	}

	return queryRequest(db, newID)
}

// DeleteBinnedRequests destroys binned requests forever, and forgets every
// undo step that involves them — undo may never bring back something the
// user meant to destroy.
type DeleteBinnedRequests struct{}

func (DeleteBinnedRequests) Info() commands.Info {
	return commands.Info{FunctionID: "bin.delete_permanently", Label: "Delete Forever"}
}

func (DeleteBinnedRequests) Run(db store.Session, input json.RawMessage) (any, error) {
	var payload struct {
		BinIDs []int64 `json:"bin_ids"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, err
	}
	if len(payload.BinIDs) == 0 {
		return nil, nil
	}

	// Remember which original requests these bin rows came from, so their
	// undo steps are dropped too.
	originalIDs := make([]int64, 0, len(payload.BinIDs))
	for _, binID := range payload.BinIDs {
		var originalID int64
		if err := db.QueryRow(
			`SELECT original_request_id FROM binned_requests WHERE id = ?`, binID,
		).Scan(&originalID); err != nil {
			return nil, fmt.Errorf("loading binned request %d: %w", binID, err)
		}
		originalIDs = append(originalIDs, originalID)
	}

	if err := db.Delete("binned_requests", payload.BinIDs...); err != nil {
		return nil, err
	}

	return map[string]any{"bin_ids": payload.BinIDs, "original_request_ids": originalIDs}, nil
}

// ForgetFrom tells the engine which destroyed rows to forget undo steps
// for: the bin rows and the original requests they came from.
func (DeleteBinnedRequests) ForgetFrom(result any) map[string][]int64 {
	forget := map[string][]int64{}
	if outcome, ok := result.(map[string]any); ok {
		if binIDs, ok := outcome["bin_ids"].([]int64); ok {
			forget["binned_requests"] = binIDs
		}
		if originalIDs, ok := outcome["original_request_ids"].([]int64); ok {
			forget["http_requests"] = originalIDs
		}
	}
	return forget
}
