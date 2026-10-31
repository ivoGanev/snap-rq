package requests

import (
	"database/sql"
	"encoding/json"
	"errors"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// duplicate.go — copy a request together with its tag and favourite
// memberships. Undo deletes the copy; cascades remove the copies' links.

type Duplicate struct{ tx *sql.Tx }

func NewDuplicate(tx *sql.Tx) commands.Command { return &Duplicate{tx: tx} }

func (Duplicate) Info() commands.Info {
	return commands.Info{FunctionID: "request.duplicate", Label: "Duplicate Request"}
}

// Snapshot: nothing existed before the copy.
func (Duplicate) Snapshot(_ json.RawMessage) (any, error) { return nil, nil }

func (c Duplicate) Run(input json.RawMessage) (any, error) {
	var in struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if in.ID == 0 {
		return nil, errors.New("request id is required")
	}

	original, err := queryRequest(c.tx, in.ID)
	if err != nil {
		return nil, err
	}
	order, err := nextSortOrder(c.tx, original.CollectionID)
	if err != nil {
		return nil, err
	}
	result, err := c.tx.Exec(`
		INSERT INTO http_requests (collection_id, name, url, method, body, body_type,
			request_headers, user_sort_order, user_color)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		original.CollectionID, original.Name+" (copy)", original.URL, original.Method,
		original.Body, original.BodyType, original.RequestHeaders, order, original.UserColor,
	)
	if err != nil {
		return nil, err
	}
	newID, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	// copy the tag and favourite memberships as new rows
	if _, err := c.tx.Exec(`
		INSERT INTO request_tags (request_id, tag_id, user_color)
		SELECT ?, tag_id, user_color FROM request_tags WHERE request_id = ?`, newID, in.ID); err != nil {
		return nil, err
	}
	if _, err := c.tx.Exec(`
		INSERT INTO favourite_items (favourite_collection_id, http_request_id, user_color)
		SELECT favourite_collection_id, ?, user_color FROM favourite_items WHERE http_request_id = ?`,
		newID, in.ID); err != nil {
		return nil, err
	}
	return queryRequest(c.tx, newID)
}

// Undo deletes the copy; cascades remove its copied links.
func (c Duplicate) Undo(_, result json.RawMessage) error {
	var created models.HttpRequest
	if err := json.Unmarshal(result, &created); err != nil {
		return err
	}
	_, err := c.tx.Exec(`DELETE FROM http_requests WHERE id = ?`, created.ID)
	return err
}
