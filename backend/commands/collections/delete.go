package collections

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// delete.go — remove a collection with everything the cascades take: its
// appearance, requests, responses, tag and favourite memberships. Snapshot
// saves the whole tree; undo re-inserts it, parents first, original ids.

type Delete struct{ tx *sql.Tx }

func NewDelete(tx *sql.Tx) commands.Command { return &Delete{tx: tx} }

func (Delete) Info() commands.Info {
	return commands.Info{FunctionID: "collection.delete", Label: "Delete Collection"}
}

type savedCollection struct {
	Collection models.Collection            `json:"collection"`
	Appearance *models.CollectionAppearance `json:"appearance"`
	Requests   []savedRequest               `json:"requests"`
}

type savedRequest struct {
	Request   models.HttpRequest    `json:"request"`
	Responses []models.HttpResponse `json:"responses"`
	TagLinks  []savedLink           `json:"tag_links"`
	FavLinks  []savedFavLink        `json:"fav_links"`
}

type savedLink struct {
	ID            int64   `json:"id"`
	RequestID     int64   `json:"request_id"`
	TagID         int64   `json:"tag_id"`
	UserSortOrder int     `json:"user_sort_order"`
	UserColor     *string `json:"user_color"`
}

type savedFavLink struct {
	ID                    int64   `json:"id"`
	FavouriteCollectionID int64   `json:"favourite_collection_id"`
	HTTPRequestID         int64   `json:"http_request_id"`
	UserSortOrder         int     `json:"user_sort_order"`
	UserColor             *string `json:"user_color"`
}

func (c Delete) Snapshot(input json.RawMessage) (any, error) {
	var in struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}

	saved := savedCollection{}
	err := c.tx.QueryRow(`
		SELECT col.id, col.project_id, col.name,
		       COALESCE(ca.appearance_type, 'icon'), COALESCE(ca.appearance_value, 'default')
		FROM collections col
		LEFT JOIN collection_appearances ca ON ca.collection_id = col.id
		WHERE col.id = ?`, in.ID,
	).Scan(&saved.Collection.ID, &saved.Collection.ProjectID, &saved.Collection.Name,
		&saved.Appearance.AppearanceType, &saved.Appearance.AppearanceValue)
	if err != nil {
		return nil, fmt.Errorf("loading collection: %w", err)
	}
	saved.Appearance.CollectionID = saved.Collection.ID

	// every request under the collection, each with everything under it
	rows, err := c.tx.Query(`
		SELECT id, collection_id, name, url, method, COALESCE(body, ''), body_type,
		       COALESCE(request_headers, ''), status_code, response_id, user_sort_order, user_color
		FROM http_requests WHERE collection_id = ? ORDER BY id`, in.ID)
	if err != nil {
		return nil, fmt.Errorf("loading requests: %w", err)
	}
	var requests []models.HttpRequest
	for rows.Next() {
		var req models.HttpRequest
		if err := rows.Scan(&req.ID, &req.CollectionID, &req.Name, &req.URL, &req.Method,
			&req.Body, &req.BodyType, &req.RequestHeaders,
			&req.StatusCode, &req.ResponseID, &req.UserSortOrder, &req.UserColor); err != nil {
			rows.Close()
			return nil, err
		}
		requests = append(requests, req)
	}
	rows.Close()

	for _, req := range requests {
		one := savedRequest{Request: req}
		if err := c.loadChildren(&one); err != nil {
			return nil, err
		}
		saved.Requests = append(saved.Requests, one)
	}
	return saved, nil
}

// loadChildren fills in one request's responses, tag and favourite links.
func (c Delete) loadChildren(one *savedRequest) error {
	rows, err := c.tx.Query(`
		SELECT id, request_id, COALESCE(headers, ''), status_code, COALESCE(body, ''), created_at, duration_ms
		FROM responses WHERE request_id = ? ORDER BY id`, one.Request.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var resp models.HttpResponse
		if err := rows.Scan(&resp.ID, &resp.RequestID, &resp.Headers, &resp.StatusCode,
			&resp.Body, &resp.CreatedAt, &resp.DurationMs); err != nil {
			rows.Close()
			return err
		}
		one.Responses = append(one.Responses, resp)
	}
	rows.Close()

	rows, err = c.tx.Query(`
		SELECT id, request_id, tag_id, user_sort_order, user_color
		FROM request_tags WHERE request_id = ? ORDER BY id`, one.Request.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var link savedLink
		if err := rows.Scan(&link.ID, &link.RequestID, &link.TagID, &link.UserSortOrder, &link.UserColor); err != nil {
			rows.Close()
			return err
		}
		one.TagLinks = append(one.TagLinks, link)
	}
	rows.Close()

	rows, err = c.tx.Query(`
		SELECT id, favourite_collection_id, http_request_id, user_sort_order, user_color
		FROM favourite_items WHERE http_request_id = ? ORDER BY id`, one.Request.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var link savedFavLink
		if err := rows.Scan(&link.ID, &link.FavouriteCollectionID, &link.HTTPRequestID,
			&link.UserSortOrder, &link.UserColor); err != nil {
			rows.Close()
			return err
		}
		one.FavLinks = append(one.FavLinks, link)
	}
	rows.Close()
	return nil
}

func (c Delete) Run(input json.RawMessage) (any, error) {
	var in struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if in.ID == 0 {
		return nil, errors.New("collection id is required")
	}
	_, err := c.tx.Exec(`DELETE FROM collections WHERE id = ?`, in.ID)
	return nil, err
}

// Undo re-inserts the tree: collection, appearance, then each request with
// its children — parents first, original ids.
func (c Delete) Undo(snapshot, _ json.RawMessage) error {
	var saved savedCollection
	if err := json.Unmarshal(snapshot, &saved); err != nil {
		return err
	}

	col := saved.Collection
	if _, err := c.tx.Exec(
		`INSERT INTO collections (id, project_id, name) VALUES (?, ?, ?)`,
		col.ID, col.ProjectID, col.Name,
	); err != nil {
		return err
	}
	if _, err := c.tx.Exec(
		`INSERT INTO collection_appearances (collection_id, appearance_type, appearance_value)
		 VALUES (?, ?, ?)`,
		saved.Appearance.CollectionID, saved.Appearance.AppearanceType, saved.Appearance.AppearanceValue,
	); err != nil {
		return err
	}

	for _, one := range saved.Requests {
		r := one.Request
		if _, err := c.tx.Exec(`
			INSERT INTO http_requests (id, collection_id, name, url, method, body, body_type,
				request_headers, status_code, response_id, user_sort_order, user_color)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.ID, r.CollectionID, r.Name, r.URL, r.Method, r.Body, r.BodyType,
			r.RequestHeaders, r.StatusCode, r.ResponseID, r.UserSortOrder, r.UserColor,
		); err != nil {
			return err
		}
		for _, resp := range one.Responses {
			if _, err := c.tx.Exec(`
				INSERT INTO responses (id, request_id, headers, status_code, body, created_at, duration_ms)
				VALUES (?, ?, ?, ?, ?, ?, ?)`,
				resp.ID, resp.RequestID, resp.Headers, resp.StatusCode, resp.Body,
				resp.CreatedAt, resp.DurationMs,
			); err != nil {
				return err
			}
		}
		for _, link := range one.TagLinks {
			if _, err := c.tx.Exec(`
				INSERT INTO request_tags (id, request_id, tag_id, user_sort_order, user_color)
				VALUES (?, ?, ?, ?, ?)`,
				link.ID, link.RequestID, link.TagID, link.UserSortOrder, link.UserColor,
			); err != nil {
				return err
			}
		}
		for _, link := range one.FavLinks {
			if _, err := c.tx.Exec(`
				INSERT INTO favourite_items (id, favourite_collection_id, http_request_id, user_sort_order, user_color)
				VALUES (?, ?, ?, ?, ?)`,
				link.ID, link.FavouriteCollectionID, link.HTTPRequestID, link.UserSortOrder, link.UserColor,
			); err != nil {
				return err
			}
		}
	}
	return nil
}
