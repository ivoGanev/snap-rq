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

type Update struct {
	tx *sql.Tx
	// The group a move to another collection left empty, and so deleted.
	emptiedGroup int64
}

func NewUpdate(tx *sql.Tx) commands.Command { return &Update{tx: tx} }

func (Update) Info() commands.Info {
	return commands.Info{FunctionID: "request.update", Label: "Update Request"}
}

func (c *Update) Run(input json.RawMessage) (any, error) {
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
		"request_headers": req.RequestHeaders,
	}
	var currentCollection int64
	var currentGroup sql.NullInt64
	if err := c.tx.QueryRow(
		`SELECT collection_id, group_id FROM http_requests WHERE id = ?`, req.ID,
	).Scan(&currentCollection, &currentGroup); err != nil {
		return nil, fmt.Errorf("loading request: %w", err)
	}
	if currentCollection != req.CollectionID {
		order, err := nextSortOrder(c.tx, req.CollectionID)
		if err != nil {
			return nil, err
		}
		columns["user_sort_order"] = order
		// groups belong to one collection
		columns["group_id"] = nil
	}

	query, args, err := squirrel.Update("http_requests").SetMap(columns).
		Where(squirrel.Eq{"id": req.ID}).ToSql()
	if err != nil {
		return nil, err
	}
	if _, err := c.tx.Exec(query, args...); err != nil {
		return nil, fmt.Errorf("updating request: %w", err)
	}
	if currentCollection != req.CollectionID && currentGroup.Valid {
		if err := c.dropIfEmpty(currentGroup.Int64); err != nil {
			return nil, err
		}
	}
	return queryRequest(c.tx, req.ID)
}

// dropIfEmpty deletes the group the request left when nothing is left in it,
// as dragging a group's last request out does.
func (c *Update) dropIfEmpty(groupID int64) error {
	var left int
	if err := c.tx.QueryRow(`SELECT COUNT(*) FROM http_requests WHERE group_id = ?`, groupID).Scan(&left); err != nil {
		return err
	}
	if left > 0 {
		return nil
	}
	if _, err := c.tx.Exec(`DELETE FROM request_groups WHERE id = ?`, groupID); err != nil {
		return err
	}
	c.emptiedGroup = groupID
	return nil
}

// Changes reports a group the move deleted, so the UI drops its heading; the
// UI updates the request itself from the result.
func (c *Update) Changes(_, _ json.RawMessage) []commands.RowChange {
	if c.emptiedGroup == 0 {
		return nil
	}
	return []commands.RowChange{{Component: "group", IDs: []int64{c.emptiedGroup}}}
}
