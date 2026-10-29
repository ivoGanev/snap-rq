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
	RequestUpdateCommandName     = "request.update"
	RequestDeleteCommandName     = "request.delete"
	RequestDeleteBulkCommandName = "request.delete_bulk"
)

// RegisterCommands registers this service's commands on the bus and routes
// this service's mutations through it. Phase 1 pilot: request.update; this
// increment adds request.delete / request.delete_bulk (the trash-bin move).
func (s *RequestService) RegisterCommands(bus *commands.Bus) error {
	s.bus = bus
	for _, cmd := range []*commands.Command{
		s.requestUpdateCommand(),
		s.requestDeleteCommand(),
		s.requestDeleteBulkCommand(),
	} {
		if err := bus.Register(cmd); err != nil {
			return err
		}
	}
	return nil
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

// binRequestsForCommand moves the given requests into the trash bin inside
// the command's transaction, snapshotting every cascade victim first so the
// memento can restore the full tree (request, responses, tag memberships,
// favourite memberships, usage) with original ids.
func (s *RequestService) binRequestsForCommand(ctx *commands.Context, ids []int64) error {
	for _, id := range ids {
		responseIDs, err := selectIDsTx(ctx.Tx, `SELECT id FROM responses WHERE request_id = ?`, id)
		if err != nil {
			return fmt.Errorf("listing responses of request %d: %w", id, err)
		}
		tagIDs, err := selectIDsTx(ctx.Tx, `SELECT id FROM request_tags WHERE request_id = ?`, id)
		if err != nil {
			return fmt.Errorf("listing tag memberships of request %d: %w", id, err)
		}
		favouriteIDs, err := selectIDsTx(ctx.Tx, `SELECT id FROM favourite_items WHERE http_request_id = ?`, id)
		if err != nil {
			return fmt.Errorf("listing favourite memberships of request %d: %w", id, err)
		}

		// Parent table first: capture order becomes restore order.
		if err := ctx.Snapshot("http_requests", id); err != nil {
			return err
		}
		if err := ctx.Snapshot("responses", responseIDs...); err != nil {
			return err
		}
		if err := ctx.Snapshot("request_tags", tagIDs...); err != nil {
			return err
		}
		if err := ctx.Snapshot("favourite_items", favouriteIDs...); err != nil {
			return err
		}
		// request_usage is intentionally not snapshotted: usage telemetry is
		// append-only (System category) and is never restored by undo. The
		// cascade removes it with the request; the row reappears on the next
		// send.

		binID, err := s.binRequestTx(ctx.Tx, id)
		if err != nil {
			return fmt.Errorf("binning request %d: %w", id, err)
		}
		if err := ctx.CaptureNew("binned_requests", binID); err != nil {
			return err
		}
	}
	return nil
}

// requestDeleteCommand bins a single request. Undo removes the bin row and
// restores the request with all related rows under their original ids.
func (s *RequestService) requestDeleteCommand() *commands.Command {
	return &commands.Command{
		Name:     RequestDeleteCommandName,
		Label:    "Delete Request",
		Category: commands.Undoable,
		Handler: func(ctx *commands.Context, payload json.RawMessage) (any, error) {
			var pl struct {
				ID int64 `json:"id"`
			}
			if err := json.Unmarshal(payload, &pl); err != nil {
				return nil, err
			}
			if pl.ID == 0 {
				return nil, errors.New("request id is required")
			}

			if err := s.binRequestsForCommand(ctx, []int64{pl.ID}); err != nil {
				return nil, err
			}
			ctx.Affected("request", "delete", pl.ID)
			return nil, nil
		},
	}
}

// requestDeleteBulkCommand bins many requests as ONE undo entry.
func (s *RequestService) requestDeleteBulkCommand() *commands.Command {
	return &commands.Command{
		Name:     RequestDeleteBulkCommandName,
		Label:    "Delete Requests",
		Category: commands.Undoable,
		Handler: func(ctx *commands.Context, payload json.RawMessage) (any, error) {
			var pl struct {
				IDs []int64 `json:"ids"`
			}
			if err := json.Unmarshal(payload, &pl); err != nil {
				return nil, err
			}
			if len(pl.IDs) == 0 {
				return nil, nil
			}

			if err := s.binRequestsForCommand(ctx, pl.IDs); err != nil {
				return nil, err
			}
			ctx.Affected("request", "delete", pl.IDs...)
			return nil, nil
		},
	}
}
