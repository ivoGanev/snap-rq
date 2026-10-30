package services

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// Command names are the app's public vocabulary: one day an AI agent or a
// CLI will trigger commands by these exact names.
const (
	RequestUpdateCommandName     = "request.update"
	RequestDeleteCommandName     = "request.delete"
	RequestDeleteBulkCommandName = "request.delete_bulk"
)

// RegisterCommands makes this service's commands runnable and routes its
// mutations through the history.
func (s *RequestService) RegisterCommands(history *commands.History) error {
	s.history = history
	for _, cmd := range []*commands.Command{
		s.requestUpdateCommand(),
		s.requestDeleteCommand(),
		s.requestDeleteBulkCommand(),
	} {
		if err := history.Register(cmd); err != nil {
			return err
		}
	}
	return nil
}

// requestUpdateCommand changes the user's own columns of a request. The
// send flow's columns (status_code, response_id) are never written here:
// undo must not be able to wipe out the result of a request that was sent.
func (s *RequestService) requestUpdateCommand() *commands.Command {
	return &commands.Command{
		Name:     RequestUpdateCommandName,
		Label:    "Update Request",
		Undoable: true,
		MergeKey: func(input json.RawMessage) string {
			var req models.HttpRequest
			if err := json.Unmarshal(input, &req); err != nil || req.ID == 0 {
				return ""
			}
			return fmt.Sprintf("%s:%d", RequestUpdateCommandName, req.ID)
		},
		Apply: func(tx *sql.Tx, rec *commands.Recorder, input json.RawMessage) (any, error) {
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

			if err := rec.SaveRows("http_requests", req.ID); err != nil {
				return nil, err
			}

			if err := s.execUpdateRequest(tx, req); err != nil {
				return nil, err
			}

			rec.Touched("request", "update", req.ID)
			return s.getRequestWithProjectTx(tx, req.ID)
		},
	}
}

// execUpdateRequest applies the user's own columns. Moving a request to a
// different collection appends it to the end of that collection's order;
// otherwise its position is kept. (SET expressions see the pre-update
// column values.)
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

// binRequestsForCommand moves requests into the trash bin, saving copies
// of everything that gets deleted with them (the request, its responses,
// its tag and favourite memberships) so undo can put the whole thing back.
func (s *RequestService) binRequestsForCommand(tx *sql.Tx, rec *commands.Recorder, ids []int64) error {
	for _, id := range ids {
		responseIDs, err := selectIDsTx(tx, `SELECT id FROM responses WHERE request_id = ?`, id)
		if err != nil {
			return fmt.Errorf("listing responses of request %d: %w", id, err)
		}
		tagIDs, err := selectIDsTx(tx, `SELECT id FROM request_tags WHERE request_id = ?`, id)
		if err != nil {
			return fmt.Errorf("listing tag memberships of request %d: %w", id, err)
		}
		favouriteIDs, err := selectIDsTx(tx, `SELECT id FROM favourite_items WHERE http_request_id = ?`, id)
		if err != nil {
			return fmt.Errorf("listing favourite memberships of request %d: %w", id, err)
		}

		// Parent table first: rows are put back in save order.
		if err := rec.SaveRows("http_requests", id); err != nil {
			return err
		}
		if err := rec.SaveRows("responses", responseIDs...); err != nil {
			return err
		}
		if err := rec.SaveRows("request_tags", tagIDs...); err != nil {
			return err
		}
		if err := rec.SaveRows("favourite_items", favouriteIDs...); err != nil {
			return err
		}
		// request_usage is deliberately not saved: usage is telemetry, and
		// undo never brings telemetry back. The row returns on the next
		// send.

		binID, err := s.binRequestTx(tx, id)
		if err != nil {
			return fmt.Errorf("binning request %d: %w", id, err)
		}
		if err := rec.SaveNewRows("binned_requests", binID); err != nil {
			return err
		}
	}
	return nil
}

// requestDeleteCommand bins one request. Undo removes the bin row and puts
// the request, with everything that belonged to it, back where it was.
func (s *RequestService) requestDeleteCommand() *commands.Command {
	return &commands.Command{
		Name:     RequestDeleteCommandName,
		Label:    "Delete Request",
		Undoable: true,
		Apply: func(tx *sql.Tx, rec *commands.Recorder, input json.RawMessage) (any, error) {
			var payload struct {
				ID int64 `json:"id"`
			}
			if err := json.Unmarshal(input, &payload); err != nil {
				return nil, err
			}
			if payload.ID == 0 {
				return nil, errors.New("request id is required")
			}

			if err := s.binRequestsForCommand(tx, rec, []int64{payload.ID}); err != nil {
				return nil, err
			}
			rec.Touched("request", "delete", payload.ID)
			return nil, nil
		},
	}
}

// requestDeleteBulkCommand bins many requests as ONE undo step.
func (s *RequestService) requestDeleteBulkCommand() *commands.Command {
	return &commands.Command{
		Name:     RequestDeleteBulkCommandName,
		Label:    "Delete Requests",
		Undoable: true,
		Apply: func(tx *sql.Tx, rec *commands.Recorder, input json.RawMessage) (any, error) {
			var payload struct {
				IDs []int64 `json:"ids"`
			}
			if err := json.Unmarshal(input, &payload); err != nil {
				return nil, err
			}
			if len(payload.IDs) == 0 {
				return nil, nil
			}

			if err := s.binRequestsForCommand(tx, rec, payload.IDs); err != nil {
				return nil, err
			}
			rec.Touched("request", "delete", payload.IDs...)
			return nil, nil
		},
	}
}
