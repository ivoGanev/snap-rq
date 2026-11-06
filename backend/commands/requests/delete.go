package requests

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Masterminds/squirrel"
	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// delete.go — move requests to the trash. Snapshot saves the requests and
// everything the cascades remove with them; undo re-inserts the whole tree
// under the original ids and removes the trash rows.

type Delete struct{ tx *sql.Tx }

func NewDelete(tx *sql.Tx) commands.Command { return &Delete{tx: tx} }

func (Delete) Info() commands.Info {
	return commands.Info{FunctionID: "request.delete", Label: "Delete Requests"}
}

type deleteInput struct {
	IDs []int64 `json:"ids"`
}

// savedRequest is one request with everything that dies with it.
type savedRequest struct {
	Request   models.HttpRequest    `json:"request"`
	Responses []models.HttpResponse `json:"responses"`
	TagLinks  []savedTagLink        `json:"tag_links"`
	FavLinks  []savedFavLink        `json:"fav_links"`
}

type savedTagLink struct {
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
	var in deleteInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}

	var saved []savedRequest
	for _, id := range in.IDs {
		req, err := queryRequest(c.tx, id)
		if err != nil {
			return nil, err
		}
		one := savedRequest{Request: req}

		rows, err := c.tx.Query(
			`SELECT id, request_id, COALESCE(headers, ''), status_code, COALESCE(body, ''), created_at, duration_ms
			 FROM responses WHERE request_id = ? ORDER BY id`, id)
		if err != nil {
			return nil, fmt.Errorf("loading responses: %w", err)
		}
		for rows.Next() {
			var resp models.HttpResponse
			if err := rows.Scan(&resp.ID, &resp.RequestID, &resp.Headers, &resp.StatusCode,
				&resp.Body, &resp.CreatedAt, &resp.DurationMs); err != nil {
				rows.Close()
				return nil, err
			}
			one.Responses = append(one.Responses, resp)
		}
		rows.Close()

		rows, err = c.tx.Query(
			`SELECT id, request_id, tag_id, user_sort_order, user_color
			 FROM request_tags WHERE request_id = ? ORDER BY id`, id)
		if err != nil {
			return nil, fmt.Errorf("loading tag links: %w", err)
		}
		for rows.Next() {
			var link savedTagLink
			if err := rows.Scan(&link.ID, &link.RequestID, &link.TagID, &link.UserSortOrder, &link.UserColor); err != nil {
				rows.Close()
				return nil, err
			}
			one.TagLinks = append(one.TagLinks, link)
		}
		rows.Close()

		rows, err = c.tx.Query(
			`SELECT id, favourite_collection_id, http_request_id, user_sort_order, user_color
			 FROM favourite_items WHERE http_request_id = ? ORDER BY id`, id)
		if err != nil {
			return nil, fmt.Errorf("loading favourite links: %w", err)
		}
		for rows.Next() {
			var link savedFavLink
			if err := rows.Scan(&link.ID, &link.FavouriteCollectionID, &link.HTTPRequestID,
				&link.UserSortOrder, &link.UserColor); err != nil {
				rows.Close()
				return nil, err
			}
			one.FavLinks = append(one.FavLinks, link)
		}
		rows.Close()

		saved = append(saved, one)
	}
	return saved, nil
}

func (c Delete) Run(input json.RawMessage) (any, error) {
	var in deleteInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if len(in.IDs) == 0 {
		return nil, nil
	}

	for _, id := range in.IDs {
		if err := c.writeBinRow(id); err != nil {
			return nil, err
		}
	}
	query, args, err := squirrel.Delete("http_requests").Where(squirrel.Eq{"id": in.IDs}).ToSql()
	if err != nil {
		return nil, err
	}
	_, err = c.tx.Exec(query, args...)
	return nil, err
}

// writeBinRow saves the trash copy while the request still exists.
func (c Delete) writeBinRow(id int64) error {
	req, err := queryRequest(c.tx, id)
	if err != nil {
		return err
	}

	// the trash snapshot stores tag and favourite names, for the restore UI
	snapshot := models.BinnedRequestSnapshot{}
	rows, err := c.tx.Query(`
		SELECT t.id, t.name FROM request_tags rt JOIN tags t ON t.id = rt.tag_id
		WHERE rt.request_id = ? ORDER BY t.name`, id)
	if err != nil {
		return fmt.Errorf("loading tags: %w", err)
	}
	for rows.Next() {
		var tag models.BinnedTagSnapshot
		if err := rows.Scan(&tag.ID, &tag.Name); err != nil {
			rows.Close()
			return err
		}
		snapshot.Tags = append(snapshot.Tags, tag)
	}
	rows.Close()

	rows, err = c.tx.Query(`
		SELECT fc.id, fc.name FROM favourite_items fi
		JOIN favourite_collections fc ON fc.id = fi.favourite_collection_id
		WHERE fi.http_request_id = ? ORDER BY fc.name`, id)
	if err != nil {
		return fmt.Errorf("loading favourites: %w", err)
	}
	for rows.Next() {
		var fav models.BinnedFavouriteSnapshot
		if err := rows.Scan(&fav.ID, &fav.Name); err != nil {
			rows.Close()
			return err
		}
		snapshot.Favourites = append(snapshot.Favourites, fav)
	}
	rows.Close()

	// responses are part of the trash snapshot too
	rows, err = c.tx.Query(`
		SELECT id, COALESCE(headers, ''), status_code, COALESCE(body, ''), created_at, duration_ms
		FROM responses WHERE request_id = ? ORDER BY id`, id)
	if err != nil {
		return fmt.Errorf("loading responses: %w", err)
	}
	for rows.Next() {
		var resp models.BinnedResponseSnapshot
		if err := rows.Scan(&resp.ID, &resp.Headers, &resp.StatusCode, &resp.Body,
			&resp.CreatedAt, &resp.DurationMs); err != nil {
			rows.Close()
			return err
		}
		snapshot.Responses = append(snapshot.Responses, resp)
	}
	rows.Close()

	snapshotJSON, err := snapshot.MarshalJSON()
	if err != nil {
		return fmt.Errorf("serialising the snapshot: %w", err)
	}

	var collectionName string
	if err := c.tx.QueryRow(
		`SELECT name FROM collections WHERE id = ?`, req.CollectionID,
	).Scan(&collectionName); err != nil {
		collectionName = ""
	}

	_, err = c.tx.Exec(`
		INSERT INTO binned_requests (
			original_request_id, project_id, collection_id, original_collection_id, original_collection_name,
			name, url, method, body, body_type, request_headers, status_code, response_id, snapshot_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.ID, req.ProjectID, req.CollectionID, req.CollectionID, collectionName,
		req.Name, req.URL, req.Method, req.Body, normalizeBodyType(req.BodyType),
		req.RequestHeaders, req.StatusCode, 0, string(snapshotJSON),
	)
	return err
}

// Undo re-inserts the saved tree under the original ids and removes the
// trash rows this command wrote.
func (c Delete) Undo(snapshot, _ json.RawMessage) error {
	var saved []savedRequest
	if err := json.Unmarshal(snapshot, &saved); err != nil {
		return err
	}
	for _, one := range saved {
		r := one.Request
		if _, err := c.tx.Exec(`
			INSERT INTO http_requests (
				id, collection_id, name, url, method, body, body_type, request_headers,
				status_code, response_id, user_sort_order, user_color
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.ID, r.CollectionID, r.Name, r.URL, r.Method, r.Body, r.BodyType, r.RequestHeaders,
			r.StatusCode, r.ResponseID, r.UserSortOrder, r.UserColor,
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
		if _, err := c.tx.Exec(
			`DELETE FROM binned_requests WHERE original_request_id = ?`, r.ID,
		); err != nil {
			return err
		}
	}
	return nil
}

func (Delete) Changes(input, _ json.RawMessage) []commands.RowChange {
	return []commands.RowChange{
		{Component: "request", IDs: commands.IDsOf(input, "ids")},
		{Component: "bin"},
	}
}

var _ = errors.New
