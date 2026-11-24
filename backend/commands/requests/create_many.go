package requests

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// create_many.go — create several requests in one transaction and one undo
// step. Each is appended to its collection in the order given, so the order
// doesn't depend on which of several calls arrives first.

type CreateMany struct{ tx *sql.Tx }

func NewCreateMany(tx *sql.Tx) commands.Command { return &CreateMany{tx: tx} }

func (CreateMany) Info() commands.Info {
	return commands.Info{FunctionID: "request.create_many", Label: "Create Requests"}
}

func (CreateMany) Snapshot(_ json.RawMessage) (any, error) { return nil, nil }

func (c CreateMany) Run(input json.RawMessage) (any, error) {
	var in struct {
		Requests []json.RawMessage `json:"requests"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if len(in.Requests) == 0 {
		return nil, errors.New("requests must not be empty")
	}
	created := make([]models.HttpRequest, 0, len(in.Requests))
	for i, raw := range in.Requests {
		req, err := c.create(raw)
		if err != nil {
			return nil, fmt.Errorf("request %d of %d: %w", i+1, len(in.Requests), err)
		}
		created = append(created, req)
	}
	return created, nil
}

func (c CreateMany) create(raw json.RawMessage) (models.HttpRequest, error) {
	var req models.HttpRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) && typeErr.Type.Kind() == reflect.String {
			return req, fmt.Errorf("%s must be a string, not %s; send JSON bodies as text", typeErr.Field, typeErr.Value)
		}
		return req, err
	}
	if req.CollectionID == 0 {
		return req, errors.New("collection_id is required")
	}
	req.Method = strings.ToUpper(strings.TrimSpace(req.Method))
	if req.Method == "" {
		req.Method = "GET"
	}
	order, err := nextSortOrder(c.tx, req.CollectionID)
	if err != nil {
		return req, err
	}
	result, err := c.tx.Exec(
		`INSERT INTO http_requests (collection_id, name, url, method, body, body_type, body_form,
			request_headers, user_sort_order) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.CollectionID, req.Name, req.URL, req.Method, req.Body,
		normalizeBodyType(req.BodyType), req.BodyForm, req.RequestHeaders, order)
	if err != nil {
		return req, fmt.Errorf("creating it in collection %d: %w", req.CollectionID, err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return req, err
	}
	return queryRequest(c.tx, id)
}

func (c CreateMany) Undo(_, result json.RawMessage) error {
	var created []models.HttpRequest
	if err := json.Unmarshal(result, &created); err != nil {
		return err
	}
	for _, req := range created {
		if _, err := c.tx.Exec(`DELETE FROM http_requests WHERE id = ?`, req.ID); err != nil {
			return err
		}
	}
	return nil
}

func (CreateMany) Changes(_, result json.RawMessage) []commands.RowChange {
	return []commands.RowChange{{Component: "request", IDs: commands.IDsOf(result, "id")}}
}
