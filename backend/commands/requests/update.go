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

// update.go — change the user's own columns of a request. The send flow's
// columns (status_code, response_id) are never written here, so undo can
// never wipe out the result of a request that was sent.

type Update struct{ tx *sql.Tx }

func NewUpdate(tx *sql.Tx) commands.Command { return &Update{tx: tx} }

func (Update) Info() commands.Info {
	return commands.Info{FunctionID: "request.update", Label: "Update Request"}
}

// oldValues is what update.go saves for undo: the previous values of
// exactly the columns this command writes.
type oldValues struct {
	ID             int64  `json:"id"`
	CollectionID   int64  `json:"collection_id"`
	Name           string `json:"name"`
	URL            string `json:"url"`
	Method         string `json:"method"`
	Body           string `json:"body"`
	BodyType       string `json:"body_type"`
	RequestHeaders string `json:"request_headers"`
	UserSortOrder  int    `json:"user_sort_order"`
}

func (c Update) Snapshot(input json.RawMessage) (any, error) {
	var req models.HttpRequest
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	var old oldValues
	err := c.tx.QueryRow(
		`SELECT id, collection_id, name, url, method, COALESCE(body, ''), body_type,
		        COALESCE(request_headers, ''), user_sort_order
		 FROM http_requests WHERE id = ?`, req.ID,
	).Scan(&old.ID, &old.CollectionID, &old.Name, &old.URL, &old.Method,
		&old.Body, &old.BodyType, &old.RequestHeaders, &old.UserSortOrder)
	if err != nil {
		return nil, fmt.Errorf("loading request: %w", err)
	}
	return old, nil
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

// Undo writes the previous values back — only these columns, so a send's
// status update in between survives.
func (c Update) Undo(snapshot, _ json.RawMessage) error {
	var old oldValues
	if err := json.Unmarshal(snapshot, &old); err != nil {
		return err
	}
	_, err := c.tx.Exec(
		`UPDATE http_requests SET collection_id = ?, name = ?, url = ?, method = ?,
		     body = ?, body_type = ?, request_headers = ?, user_sort_order = ?
		 WHERE id = ?`,
		old.CollectionID, old.Name, old.URL, old.Method,
		old.Body, old.BodyType, old.RequestHeaders, old.UserSortOrder, old.ID,
	)
	return err
}
