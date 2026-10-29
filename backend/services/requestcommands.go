package services

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// Command names are public vocabulary (see specs/command-framework.md):
// future external callers execute these exact names.
const (
	RequestUpdateCommandName = "request.update"
)

// RegisterCommands registers this service's commands on the bus and routes
// this service's mutations through it. The pilot Phase 1 command is
// request.update; the remaining mutations migrate in later phases.
func (s *RequestService) RegisterCommands(bus *commands.Bus) error {
	s.bus = bus
	return bus.Register(s.requestUpdateCommand())
}

// requestUpdateCommand is the pilot undoable command. Column ownership: it
// writes only user-owned columns — status_code and response_id belong to
// the send flow (a System command) and are never written here, so an undo
// can never clobber an interleaved send's status update.
func (s *RequestService) requestUpdateCommand() *commands.Command {
	return &commands.Command{
		Name:     RequestUpdateCommandName,
		Label:    "Update Request",
		Category: commands.Undoable,
		CoalesceKey: func(p json.RawMessage) string {
			var req models.HttpRequest
			if err := json.Unmarshal(p, &req); err != nil || req.ID == 0 {
				return ""
			}
			return fmt.Sprintf("%s:%d", RequestUpdateCommandName, req.ID)
		},
		Handler: func(ctx *commands.Context, payload json.RawMessage) (any, error) {
			var req models.HttpRequest
			if err := json.Unmarshal(payload, &req); err != nil {
				return nil, err
			}
			if req.ID == 0 {
				return nil, errors.New("request id is required")
			}
			if req.CollectionID == 0 {
				return nil, errors.New("collection id is required")
			}

			if err := ctx.Snapshot("http_requests", req.ID); err != nil {
				return nil, err
			}

			if err := s.execUpdateRequest(ctx.Tx, req); err != nil {
				return nil, err
			}

			ctx.Affected("request", "update", req.ID)
			return s.getRequestWithProjectTx(ctx.Tx, req.ID)
		},
	}
}

// execUpdateRequest applies the user-owned columns of a request. Moving to a
// different collection appends the request to the target collection's user
// order; otherwise its position is preserved. (SET expressions see the
// pre-update column values.)
func (s *RequestService) execUpdateRequest(q execer, req models.HttpRequest) error {
	result, err := q.Exec(
		`UPDATE http_requests
		 SET collection_id = ?, name = ?, url = ?, method = ?, body = ?, body_type = ?, request_headers = ?,
		     user_sort_order = CASE
		       WHEN collection_id = ? THEN user_sort_order
		       ELSE (SELECT COALESCE(MAX(user_sort_order), -1) + 1 FROM http_requests h2 WHERE h2.collection_id = ?)
		     END
		 WHERE id = ?`,
		req.CollectionID, req.Name, req.URL, req.Method, req.Body, normalizeBodyType(req.BodyType),
		req.RequestHeaders, req.CollectionID, req.CollectionID, req.ID,
	)
	if err != nil {
		return fmt.Errorf("updating request: %w", err)
	}
	rows, err := result.RowsAffected()
	if err == nil && rows == 0 {
		return fmt.Errorf("request %d not found", req.ID)
	}
	return nil
}

type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}
