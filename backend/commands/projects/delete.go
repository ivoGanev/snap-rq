package projects

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// delete.go — remove a project with its entire tree: collections (with
// appearances, requests, responses, tag and favourite links), environments
// (with variables), favourite collections (with appearances and items),
// tags (with appearances and request links). Snapshot saves all of it;
// undo re-inserts everything under the original ids, parents first.

type Delete struct{ tx *sql.Tx }

func NewDelete(tx *sql.Tx) commands.Command { return &Delete{tx: tx} }

func (Delete) Info() commands.Info {
	return commands.Info{FunctionID: "project.delete", Label: "Delete Project"}
}

type savedProject struct {
	Project      models.Project               `json:"project"`
	Collections  []savedCollection            `json:"collections"`
	Requests     []savedRequest               `json:"requests"`
	Environments []models.Environment         `json:"environments"`
	Variables    []models.EnvironmentVariable `json:"variables"`
	Favourites   []savedFavouriteCollection   `json:"favourites"`
	FavItems     []savedFavItem               `json:"fav_items"`
	Tags         []savedTag                   `json:"tags"`
	RequestTags  []savedRequestTag            `json:"request_tags"`
}

type savedCollection struct {
	ID              int64  `json:"id"`
	ProjectID       int64  `json:"project_id"`
	Name            string `json:"name"`
	AppearanceType  string `json:"appearance_type"`
	AppearanceValue string `json:"appearance_value"`
}

type savedRequest struct {
	ID             int64           `json:"id"`
	CollectionID   int64           `json:"collection_id"`
	Name           string          `json:"name"`
	URL            string          `json:"url"`
	Method         string          `json:"method"`
	Body           string          `json:"body"`
	BodyType       string          `json:"body_type"`
	RequestHeaders string          `json:"request_headers"`
	StatusCode     int             `json:"status_code"`
	ResponseID     int64           `json:"response_id"`
	UserSortOrder  int             `json:"user_sort_order"`
	UserColor      *string         `json:"user_color"`
	Responses      []savedResponse `json:"responses"`
}

type savedResponse struct {
	ID         int64  `json:"id"`
	RequestID  int64  `json:"request_id"`
	Headers    string `json:"headers"`
	StatusCode int    `json:"status_code"`
	Body       string `json:"body"`
	CreatedAt  string `json:"created_at"`
	DurationMs int64  `json:"duration_ms"`
}

type savedFavouriteCollection struct {
	ID              int64  `json:"id"`
	ProjectID       int64  `json:"project_id"`
	Name            string `json:"name"`
	CreatedAt       string `json:"created_at"`
	AppearanceType  string `json:"appearance_type"`
	AppearanceValue string `json:"appearance_value"`
}

type savedFavItem struct {
	ID                    int64   `json:"id"`
	FavouriteCollectionID int64   `json:"favourite_collection_id"`
	HTTPRequestID         int64   `json:"http_request_id"`
	UserSortOrder         int     `json:"user_sort_order"`
	UserColor             *string `json:"user_color"`
}

type savedTag struct {
	ID              int64  `json:"id"`
	ProjectID       int64  `json:"project_id"`
	Name            string `json:"name"`
	AppearanceType  string `json:"appearance_type"`
	AppearanceValue string `json:"appearance_value"`
}

type savedRequestTag struct {
	ID            int64   `json:"id"`
	RequestID     int64   `json:"request_id"`
	TagID         int64   `json:"tag_id"`
	UserSortOrder int     `json:"user_sort_order"`
	UserColor     *string `json:"user_color"`
}

func (c Delete) Snapshot(input json.RawMessage) (any, error) {
	var in struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}

	saved := savedProject{}
	if err := c.tx.QueryRow(
		`SELECT id, profile_id, name FROM projects WHERE id = ?`, in.ID,
	).Scan(&saved.Project.ID, &saved.Project.ProfileID, &saved.Project.Name); err != nil {
		return nil, errors.New("project not found")
	}

	// collections with appearances
	rows, err := c.tx.Query(`
		SELECT col.id, col.project_id, col.name,
		       COALESCE(ca.appearance_type, ''), COALESCE(ca.appearance_value, '')
		FROM collections col
		LEFT JOIN collection_appearances ca ON ca.collection_id = col.id
		WHERE col.project_id = ? ORDER BY col.id`, in.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var col savedCollection
		if err := rows.Scan(&col.ID, &col.ProjectID, &col.Name,
			&col.AppearanceType, &col.AppearanceValue); err != nil {
			rows.Close()
			return nil, err
		}
		saved.Collections = append(saved.Collections, col)
	}
	rows.Close()

	// requests with responses (tag and favourite links come with the tags
	// and favourites below)
	rows, err = c.tx.Query(`
		SELECT id, collection_id, name, url, method, COALESCE(body, ''), body_type,
		       COALESCE(request_headers, ''), status_code, response_id, user_sort_order, user_color
		FROM http_requests WHERE collection_id IN (SELECT id FROM collections WHERE project_id = ?)
		ORDER BY id`, in.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var req savedRequest
		if err := rows.Scan(&req.ID, &req.CollectionID, &req.Name, &req.URL, &req.Method,
			&req.Body, &req.BodyType, &req.RequestHeaders,
			&req.StatusCode, &req.ResponseID, &req.UserSortOrder, &req.UserColor); err != nil {
			rows.Close()
			return nil, err
		}
		saved.Requests = append(saved.Requests, req)
	}
	rows.Close()

	for i, req := range saved.Requests {
		respRows, err := c.tx.Query(`
			SELECT id, request_id, COALESCE(headers, ''), status_code, COALESCE(body, ''), created_at, duration_ms
			FROM responses WHERE request_id = ? ORDER BY id`, req.ID)
		if err != nil {
			return nil, err
		}
		for respRows.Next() {
			var resp savedResponse
			if err := respRows.Scan(&resp.ID, &resp.RequestID, &resp.Headers, &resp.StatusCode,
				&resp.Body, &resp.CreatedAt, &resp.DurationMs); err != nil {
				respRows.Close()
				return nil, err
			}
			saved.Requests[i].Responses = append(saved.Requests[i].Responses, resp)
		}
		respRows.Close()
	}

	// environments with variables
	rows, err = c.tx.Query(
		`SELECT id, project_id, name, created_at FROM environments WHERE project_id = ? ORDER BY id`, in.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var env models.Environment
		if err := rows.Scan(&env.ID, &env.ProjectID, &env.Name, &env.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		saved.Environments = append(saved.Environments, env)
	}
	rows.Close()

	rows, err = c.tx.Query(`
		SELECT ev.id, ev.environment_id, ev.key, ev.value FROM environment_variables ev
		JOIN environments e ON e.id = ev.environment_id WHERE e.project_id = ? ORDER BY ev.id`, in.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var v models.EnvironmentVariable
		if err := rows.Scan(&v.ID, &v.EnvironmentID, &v.Key, &v.Value); err != nil {
			rows.Close()
			return nil, err
		}
		saved.Variables = append(saved.Variables, v)
	}
	rows.Close()

	// favourite collections with appearances and items
	rows, err = c.tx.Query(`
		SELECT fc.id, fc.project_id, fc.name, fc.created_at,
		       COALESCE(fa.appearance_type, ''), COALESCE(fa.appearance_value, '')
		FROM favourite_collections fc
		LEFT JOIN favourite_appearances fa ON fa.favourite_collection_id = fc.id
		WHERE fc.project_id = ? ORDER BY fc.id`, in.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var fav savedFavouriteCollection
		if err := rows.Scan(&fav.ID, &fav.ProjectID, &fav.Name, &fav.CreatedAt,
			&fav.AppearanceType, &fav.AppearanceValue); err != nil {
			rows.Close()
			return nil, err
		}
		saved.Favourites = append(saved.Favourites, fav)
	}
	rows.Close()

	rows, err = c.tx.Query(`
		SELECT fi.id, fi.favourite_collection_id, fi.http_request_id, fi.user_sort_order, fi.user_color
		FROM favourite_items fi
		JOIN favourite_collections fc ON fc.id = fi.favourite_collection_id
		WHERE fc.project_id = ? ORDER BY fi.id`, in.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var item savedFavItem
		if err := rows.Scan(&item.ID, &item.FavouriteCollectionID, &item.HTTPRequestID,
			&item.UserSortOrder, &item.UserColor); err != nil {
			rows.Close()
			return nil, err
		}
		saved.FavItems = append(saved.FavItems, item)
	}
	rows.Close()

	// tags with appearances and request links
	rows, err = c.tx.Query(`
		SELECT t.id, t.project_id, t.name,
		       COALESCE(ta.appearance_type, ''), COALESCE(ta.appearance_value, '')
		FROM tags t
		LEFT JOIN tag_appearances ta ON ta.tag_id = t.id
		WHERE t.project_id = ? ORDER BY t.id`, in.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var tag savedTag
		if err := rows.Scan(&tag.ID, &tag.ProjectID, &tag.Name,
			&tag.AppearanceType, &tag.AppearanceValue); err != nil {
			rows.Close()
			return nil, err
		}
		saved.Tags = append(saved.Tags, tag)
	}
	rows.Close()

	rows, err = c.tx.Query(`
		SELECT rt.id, rt.request_id, rt.tag_id, rt.user_sort_order, rt.user_color
		FROM request_tags rt
		JOIN tags t ON t.id = rt.tag_id
		WHERE t.project_id = ? ORDER BY rt.id`, in.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var link savedRequestTag
		if err := rows.Scan(&link.ID, &link.RequestID, &link.TagID,
			&link.UserSortOrder, &link.UserColor); err != nil {
			rows.Close()
			return nil, err
		}
		saved.RequestTags = append(saved.RequestTags, link)
	}
	rows.Close()

	return saved, nil
}

func (c Delete) Run(input json.RawMessage) (any, error) {
	var in struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if in.ID == 0 {
		return nil, errors.New("project id is required")
	}
	_, err := c.tx.Exec(`DELETE FROM projects WHERE id = ?`, in.ID)
	return nil, err
}

// Undo re-inserts the whole tree, parents first, under the original ids.
func (c Delete) Undo(snapshot, _ json.RawMessage) error {
	var saved savedProject
	if err := json.Unmarshal(snapshot, &saved); err != nil {
		return err
	}

	if _, err := c.tx.Exec(
		`INSERT INTO projects (id, profile_id, name) VALUES (?, ?, ?)`,
		saved.Project.ID, saved.Project.ProfileID, saved.Project.Name,
	); err != nil {
		return err
	}
	for _, col := range saved.Collections {
		if _, err := c.tx.Exec(
			`INSERT INTO collections (id, project_id, name) VALUES (?, ?, ?)`,
			col.ID, col.ProjectID, col.Name,
		); err != nil {
			return err
		}
		if col.AppearanceType != "" {
			if _, err := c.tx.Exec(
				`INSERT INTO collection_appearances (collection_id, appearance_type, appearance_value)
				 VALUES (?, ?, ?)`, col.ID, col.AppearanceType, col.AppearanceValue,
			); err != nil {
				return err
			}
		}
	}
	for _, req := range saved.Requests {
		if _, err := c.tx.Exec(`
			INSERT INTO http_requests (id, collection_id, name, url, method, body, body_type,
				request_headers, status_code, response_id, user_sort_order, user_color)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			req.ID, req.CollectionID, req.Name, req.URL, req.Method, req.Body, req.BodyType,
			req.RequestHeaders, req.StatusCode, req.ResponseID, req.UserSortOrder, req.UserColor,
		); err != nil {
			return err
		}
		for _, resp := range req.Responses {
			if _, err := c.tx.Exec(`
				INSERT INTO responses (id, request_id, headers, status_code, body, created_at, duration_ms)
				VALUES (?, ?, ?, ?, ?, ?, ?)`,
				resp.ID, resp.RequestID, resp.Headers, resp.StatusCode, resp.Body,
				resp.CreatedAt, resp.DurationMs,
			); err != nil {
				return err
			}
		}
	}
	for _, env := range saved.Environments {
		if _, err := c.tx.Exec(
			`INSERT INTO environments (id, project_id, name, created_at) VALUES (?, ?, ?, ?)`,
			env.ID, env.ProjectID, env.Name, env.CreatedAt,
		); err != nil {
			return err
		}
	}
	for _, v := range saved.Variables {
		if _, err := c.tx.Exec(
			`INSERT INTO environment_variables (id, environment_id, key, value) VALUES (?, ?, ?, ?)`,
			v.ID, v.EnvironmentID, v.Key, v.Value,
		); err != nil {
			return err
		}
	}
	for _, fav := range saved.Favourites {
		if _, err := c.tx.Exec(
			`INSERT INTO favourite_collections (id, project_id, name, created_at) VALUES (?, ?, ?, ?)`,
			fav.ID, fav.ProjectID, fav.Name, fav.CreatedAt,
		); err != nil {
			return err
		}
		if fav.AppearanceType != "" {
			if _, err := c.tx.Exec(
				`INSERT INTO favourite_appearances (favourite_collection_id, appearance_type, appearance_value)
				 VALUES (?, ?, ?)`, fav.ID, fav.AppearanceType, fav.AppearanceValue,
			); err != nil {
				return err
			}
		}
	}
	for _, item := range saved.FavItems {
		if _, err := c.tx.Exec(
			`INSERT INTO favourite_items (id, favourite_collection_id, http_request_id, user_sort_order, user_color)
			 VALUES (?, ?, ?, ?, ?)`,
			item.ID, item.FavouriteCollectionID, item.HTTPRequestID, item.UserSortOrder, item.UserColor,
		); err != nil {
			return err
		}
	}
	for _, tag := range saved.Tags {
		if _, err := c.tx.Exec(
			`INSERT INTO tags (id, project_id, name) VALUES (?, ?, ?)`,
			tag.ID, tag.ProjectID, tag.Name,
		); err != nil {
			return err
		}
		if tag.AppearanceType != "" {
			if _, err := c.tx.Exec(
				`INSERT INTO tag_appearances (tag_id, appearance_type, appearance_value) VALUES (?, ?, ?)`,
				tag.ID, tag.AppearanceType, tag.AppearanceValue,
			); err != nil {
				return err
			}
		}
	}
	for _, link := range saved.RequestTags {
		if _, err := c.tx.Exec(
			`INSERT INTO request_tags (id, request_id, tag_id, user_sort_order, user_color)
			 VALUES (?, ?, ?, ?, ?)`,
			link.ID, link.RequestID, link.TagID, link.UserSortOrder, link.UserColor,
		); err != nil {
			return err
		}
	}
	return nil
}

var _ = fmt.Sprintf
