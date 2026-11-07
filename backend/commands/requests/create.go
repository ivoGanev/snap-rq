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

// create.go — save a new request, appended to its collection's order.
// Undo removes it (the created id comes back in the result).

type Create struct{ tx *sql.Tx }

func NewCreate(tx *sql.Tx) commands.Command { return &Create{tx: tx} }

func (Create) Info() commands.Info {
	return commands.Info{FunctionID: "request.create", Label: "Create Request"}
}

// Snapshot: nothing existed before a create.
func (Create) Snapshot(_ json.RawMessage) (any, error) { return nil, nil }

func (c Create) Run(input json.RawMessage) (any, error) {
	var req models.HttpRequest
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	if req.CollectionID == 0 {
		return nil, errors.New("collection id is required")
	}

	order, err := nextSortOrder(c.tx, req.CollectionID)
	if err != nil {
		return nil, err
	}
	query, args, err := squirrel.Insert("http_requests").Columns(
		"collection_id", "name", "url", "method", "body", "body_type", "body_form",
		"request_headers", "user_sort_order",
	).Values(
		req.CollectionID, req.Name, req.URL, req.Method, req.Body,
		normalizeBodyType(req.BodyType), req.BodyForm, req.RequestHeaders, order,
	).ToSql()
	if err != nil {
		return nil, err
	}
	result, err := c.tx.Exec(query, args...)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return queryRequest(c.tx, id)
}

// Undo deletes the created request; cascades remove anything attached to it.
func (c Create) Undo(_, result json.RawMessage) error {
	var created models.HttpRequest
	if err := json.Unmarshal(result, &created); err != nil {
		return err
	}
	_, err := c.tx.Exec(`DELETE FROM http_requests WHERE id = ?`, created.ID)
	return err
}

func (Create) Changes(_, result json.RawMessage) []commands.RowChange {
	return []commands.RowChange{{Component: "request", IDs: commands.IDsOf(result, "id")}}
}
