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

// update.go — change the user's own columns of a request. Deliberately not
// undoable: editing is continuous typing and the text editors have their
// own natural undo. The send flow's columns (status_code, response_id) are
// never written here.

type Update struct{ tx *sql.Tx }

func NewUpdate(tx *sql.Tx) commands.Command { return &Update{tx: tx} }

func (Update) Info() commands.Info {
	return commands.Info{FunctionID: "request.update", Label: "Update Request"}
}

func (c Update) Run(input json.RawMessage) (any, error) {
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

	// a move to another collection appends to the target's order
	columns := map[string]any{
		"collection_id":   req.CollectionID,
		"name":            req.Name,
		"url":             req.URL,
		"method":          req.Method,
		"body":            req.Body,
		"body_type":       normalizeBodyType(req.BodyType),
		"body_form":       req.BodyForm,
		"body_mode":       normalizeBodyMode(req.BodyMode, req.BodyType, req.BodyForm),
		"request_headers": req.RequestHeaders,
	}
	var currentCollection int64
	if err := c.tx.QueryRow(
		`SELECT collection_id FROM http_requests WHERE id = ?`, req.ID,
	).Scan(&currentCollection); err != nil {
		return nil, fmt.Errorf("loading request: %w", err)
	}
	if currentCollection != req.CollectionID {
		order, err := nextSortOrder(c.tx, req.CollectionID)
		if err != nil {
			return nil, err
		}
		columns["user_sort_order"] = order
	}

	query, args, err := squirrel.Update("http_requests").SetMap(columns).
		Where(squirrel.Eq{"id": req.ID}).ToSql()
	if err != nil {
		return nil, err
	}
	if _, err := c.tx.Exec(query, args...); err != nil {
		return nil, fmt.Errorf("updating request: %w", err)
	}
	return queryRequest(c.tx, req.ID)
}
