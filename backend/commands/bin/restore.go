package bin

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// restore.go — recreate binned requests into the given collections, with
// new ids, from the trash snapshots. Undoable as a whole: undo deletes the
// recreated rows and puts the trash rows back.

type Restore struct{ tx *sql.Tx }

func NewRestore(tx *sql.Tx) commands.Command { return &Restore{tx: tx} }

func (Restore) Info() commands.Info {
	return commands.Info{FunctionID: "bin.restore", Label: "Restore Requests"}
}

type restoreInput struct {
	Items []struct {
		BinID        int64 `json:"bin_id"`
		CollectionID int64 `json:"collection_id"`
	} `json:"items"`
}

// savedBinRow is one trash row as it was, for undo.
type savedBinRow struct {
	ID                     int64  `json:"id"`
	OriginalRequestID      int64  `json:"original_request_id"`
	ProjectID              int64  `json:"project_id"`
	CollectionID           int64  `json:"collection_id"`
	OriginalCollectionID   int64  `json:"original_collection_id"`
	OriginalCollectionName string `json:"original_collection_name"`
	Name                   string `json:"name"`
	URL                    string `json:"url"`
	Method                 string `json:"method"`
	Body                   string `json:"body"`
	BodyType               string `json:"body_type"`
	RequestHeaders         string `json:"request_headers"`
	StatusCode             int    `json:"status_code"`
	SnapshotJSON           string `json:"snapshot_json"`
}

func (c Restore) Snapshot(input json.RawMessage) (any, error) {
	var in restoreInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}

	var saved []savedBinRow
	for _, item := range in.Items {
		var row savedBinRow
		err := c.tx.QueryRow(`
			SELECT id, original_request_id, project_id, collection_id, original_collection_id,
			       original_collection_name, name, url, method, COALESCE(body, ''), body_type,
			       COALESCE(request_headers, ''), status_code, snapshot_json
			FROM binned_requests WHERE id = ?`, item.BinID,
		).Scan(&row.ID, &row.OriginalRequestID, &row.ProjectID, &row.CollectionID,
			&row.OriginalCollectionID, &row.OriginalCollectionName,
			&row.Name, &row.URL, &row.Method, &row.Body, &row.BodyType,
			&row.RequestHeaders, &row.StatusCode, &row.SnapshotJSON)
		if err != nil {
			return nil, fmt.Errorf("loading bin row %d: %w", item.BinID, err)
		}
		saved = append(saved, row)
	}
	return saved, nil
}

func (c Restore) Run(input json.RawMessage) (any, error) {
	var in restoreInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}

	restored := make([]models.HttpRequest, 0, len(in.Items))
	for _, item := range in.Items {
		req, err := c.restoreOne(item.BinID, item.CollectionID)
		if err != nil {
			return nil, fmt.Errorf("restoring binned request %d: %w", item.BinID, err)
		}
		restored = append(restored, req)
	}
	return restored, nil
}

func (c Restore) restoreOne(binID, targetCollectionID int64) (models.HttpRequest, error) {
	var targetProjectID int64
	if err := c.tx.QueryRow(
		`SELECT project_id FROM collections WHERE id = ?`, targetCollectionID,
	).Scan(&targetProjectID); err != nil {
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
	if err := c.tx.QueryRow(`
		SELECT project_id, name, url, method, COALESCE(body, ''), body_type,
		       COALESCE(request_headers, ''), status_code, response_id, snapshot_json
		FROM binned_requests WHERE id = ?`, binID,
	).Scan(&binned.ProjectID, &binned.Name, &binned.URL, &binned.Method,
		&binned.Body, &binned.BodyType, &binned.RequestHeaders,
		&binned.StatusCode, &binned.ResponseID, &binned.SnapshotJSON); err != nil {
		return models.HttpRequest{}, fmt.Errorf("loading binned request: %w", err)
	}
	if binned.ProjectID != targetProjectID {
		return models.HttpRequest{}, fmt.Errorf("target collection does not belong to the binned request's project")
	}

	snapshot, err := models.UnmarshalBinnedRequestSnapshot(binned.SnapshotJSON)
	if err != nil {
		return models.HttpRequest{}, fmt.Errorf("parsing the snapshot: %w", err)
	}

	result, err := c.tx.Exec(`
		INSERT INTO http_requests (collection_id, name, url, method, body, body_type,
			request_headers, status_code, response_id, user_sort_order)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0,
			(SELECT COALESCE(MAX(user_sort_order), -1) + 1 FROM http_requests WHERE collection_id = ?))`,
		targetCollectionID, binned.Name, binned.URL, binned.Method, binned.Body,
		binned.BodyType, binned.RequestHeaders, binned.StatusCode, targetCollectionID)
	if err != nil {
		return models.HttpRequest{}, fmt.Errorf("restoring request: %w", err)
	}
	newID, err := result.LastInsertId()
	if err != nil {
		return models.HttpRequest{}, err
	}

	// recreate the response history; map snapshot ids so the newest link stays correct
	newestResponseID := int64(0)
	for _, resp := range snapshot.Responses {
		res, err := c.tx.Exec(`
			INSERT INTO responses (request_id, headers, status_code, body, created_at, duration_ms)
			VALUES (?, ?, ?, ?, ?, ?)`,
			newID, resp.Headers, resp.StatusCode, resp.Body, resp.CreatedAt, resp.DurationMs)
		if err != nil {
			return models.HttpRequest{}, fmt.Errorf("restoring response: %w", err)
		}
		newResponseID, _ := res.LastInsertId()
		if resp.ID == binned.ResponseID {
			newestResponseID = newResponseID
		}
	}
	if newestResponseID != 0 {
		if _, err := c.tx.Exec(
			`UPDATE http_requests SET response_id = ? WHERE id = ?`, newestResponseID, newID,
		); err != nil {
			return models.HttpRequest{}, err
		}
	}

	// re-attach tags and favourites that still exist, by id then by name
	for _, tag := range snapshot.Tags {
		var tagID int64
		err := c.tx.QueryRow(
			`SELECT id FROM tags WHERE id = ? AND project_id = ?`, tag.ID, targetProjectID,
		).Scan(&tagID)
		if err == sql.ErrNoRows {
			err = c.tx.QueryRow(
				`SELECT id FROM tags WHERE name = ? AND project_id = ?`, tag.Name, targetProjectID,
			).Scan(&tagID)
		}
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return models.HttpRequest{}, fmt.Errorf("looking up tag: %w", err)
		}
		if _, err := c.tx.Exec(
			`INSERT OR IGNORE INTO request_tags (request_id, tag_id) VALUES (?, ?)`, newID, tagID,
		); err != nil {
			return models.HttpRequest{}, err
		}
	}
	for _, fav := range snapshot.Favourites {
		var favID int64
		err := c.tx.QueryRow(
			`SELECT id FROM favourite_collections WHERE id = ? AND project_id = ?`, fav.ID, targetProjectID,
		).Scan(&favID)
		if err == sql.ErrNoRows {
			err = c.tx.QueryRow(
				`SELECT id FROM favourite_collections WHERE name = ? AND project_id = ?`, fav.Name, targetProjectID,
			).Scan(&favID)
		}
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return models.HttpRequest{}, fmt.Errorf("looking up favourite: %w", err)
		}
		if _, err := c.tx.Exec(
			`INSERT OR IGNORE INTO favourite_items (favourite_collection_id, http_request_id) VALUES (?, ?)`,
			favID, newID,
		); err != nil {
			return models.HttpRequest{}, err
		}
	}

	if _, err := c.tx.Exec(`DELETE FROM binned_requests WHERE id = ?`, binID); err != nil {
		return models.HttpRequest{}, err
	}

	restored, err := queryRequest(c.tx, newID)
	if err != nil {
		return models.HttpRequest{}, err
	}
	return restored, nil
}

// queryRequest is the request loader (kept local: only this file uses it).
func queryRequest(tx *sql.Tx, id int64) (models.HttpRequest, error) {
	var req models.HttpRequest
	err := tx.QueryRow(`
		SELECT hr.id, hr.collection_id, c.project_id, hr.name, hr.url, hr.method,
		       COALESCE(hr.body, ''), hr.body_type, COALESCE(hr.request_headers, ''),
		       hr.status_code, hr.response_id, hr.user_sort_order, hr.user_color, hr.last_used_at
		FROM http_requests hr JOIN collections c ON c.id = hr.collection_id WHERE hr.id = ?`, id,
	).Scan(&req.ID, &req.CollectionID, &req.ProjectID, &req.Name, &req.URL, &req.Method,
		&req.Body, &req.BodyType, &req.RequestHeaders,
		&req.StatusCode, &req.ResponseID, &req.UserSortOrder, &req.UserColor, &req.LastUsedAt)
	if err != nil {
		return models.HttpRequest{}, fmt.Errorf("loading request: %w", err)
	}
	return req, nil
}

// Undo deletes the recreated requests (cascades remove their children and
// links) and puts the trash rows back as they were.
func (c Restore) Undo(snapshot, result json.RawMessage) error {
	var saved []savedBinRow
	if err := json.Unmarshal(snapshot, &saved); err != nil {
		return err
	}
	var restored []models.HttpRequest
	if err := json.Unmarshal(result, &restored); err != nil {
		return err
	}

	for _, req := range restored {
		if _, err := c.tx.Exec(`DELETE FROM http_requests WHERE id = ?`, req.ID); err != nil {
			return err
		}
	}
	for _, row := range saved {
		if _, err := c.tx.Exec(`
			INSERT INTO binned_requests (
				id, original_request_id, project_id, collection_id, original_collection_id,
				original_collection_name, name, url, method, body, body_type, request_headers,
				status_code, response_id, snapshot_json
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			row.ID, row.OriginalRequestID, row.ProjectID, row.CollectionID,
			row.OriginalCollectionID, row.OriginalCollectionName,
			row.Name, row.URL, row.Method, row.Body, row.BodyType, row.RequestHeaders,
			row.StatusCode, 0, row.SnapshotJSON,
		); err != nil {
			return err
		}
	}
	return nil
}

func (Restore) Changes(_, result json.RawMessage) []commands.RowChange {
	return []commands.RowChange{
		{Component: "request", IDs: commands.IDsOf(result, "id")},
		{Component: "bin"},
	}
}
