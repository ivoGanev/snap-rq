package services

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// This file is everything about commands in the services layer: the list
// of commands the app knows (RegisterCommands) and the Wails service the
// frontend uses to run them (CommandService). The undo machinery itself
// lives in backend/commands.

// Command names are the app's public vocabulary: one day an AI agent or a
// CLI will trigger commands by these exact names.
const (
	RequestUpdateCommandName             = "request.update"
	RequestDeleteCommandName             = "request.delete"
	RequestDeleteBulkCommandName         = "request.delete_bulk"
	FavouriteDeleteCollectionCommandName = "favourite_collection.delete"
	EnvironmentDeleteCommandName         = "environment.delete"
	EnvironmentVariableDeleteCommandName = "environment_variable.delete"
)

// RegisterCommands defines every command the app knows. Each one is a
// small function: save copies of the rows it will touch, change them, note
// what the user will see.
func RegisterCommands(
	history *commands.History,
	requests *RequestService,
	favourites *FavouriteService,
	environments *EnvironmentService,
	variables *EnvironmentVariableService,
) error {
	for _, cmd := range []*commands.Command{
		updateRequest(requests),
		deleteRequest(requests),
		deleteRequests(requests),
		deleteFavouriteCollection(favourites),
		deleteEnvironment(environments),
		deleteVariable(variables),
	} {
		if err := history.Register(cmd); err != nil {
			return err
		}
	}

	// Permanently deleting binned requests must also drop the undo steps
	// that involve them: undo may never bring back something the user
	// meant to destroy.
	requests.forgetUndo = history.ForgetRows
	return nil
}

// updateRequest changes the user's own columns of a request. The send
// flow's columns (status_code, response_id) are never written here: undo
// must not be able to wipe out the result of a request that was sent.
func updateRequest(s *RequestService) *commands.Command {
	return &commands.Command{
		Name:     RequestUpdateCommandName,
		Label:    "Update Request",
		Undoable: true,
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
			if err := updateRequestColumns(tx, req); err != nil {
				return nil, err
			}

			rec.Touched("request", "update", req.ID)
			return s.getRequestWithProjectTx(tx, req.ID)
		},
	}
}

// updateRequestColumns applies the user's own columns. Moving a request to
// a different collection appends it to the end of that collection's order;
// otherwise its position is kept. (SET expressions see the pre-update
// column values.)
func updateRequestColumns(tx *sql.Tx, req models.HttpRequest) error {
	result, err := tx.Exec(
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

// deleteRequest bins one request. Undo removes the bin row and puts the
// request, with everything that belonged to it, back where it was.
func deleteRequest(s *RequestService) *commands.Command {
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

			if err := binRequests(tx, rec, s, []int64{payload.ID}); err != nil {
				return nil, err
			}
			rec.Touched("request", "delete", payload.ID)
			return nil, nil
		},
	}
}

// deleteRequests bins many requests as ONE undo step.
func deleteRequests(s *RequestService) *commands.Command {
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

			if err := binRequests(tx, rec, s, payload.IDs); err != nil {
				return nil, err
			}
			rec.Touched("request", "delete", payload.IDs...)
			return nil, nil
		},
	}
}

// binRequests moves requests into the trash bin, saving copies of
// everything that gets deleted with them (the request, its responses, its
// tag and favourite memberships) so undo can put the whole thing back.
// request_usage is deliberately not saved: usage is telemetry, and undo
// never brings telemetry back. The row returns on the next send.
func binRequests(tx *sql.Tx, rec *commands.Recorder, s *RequestService, ids []int64) error {
	for _, id := range ids {
		responseIDs, err := selectIDs(tx, `SELECT id FROM responses WHERE request_id = ?`, id)
		if err != nil {
			return fmt.Errorf("listing responses of request %d: %w", id, err)
		}
		tagIDs, err := selectIDs(tx, `SELECT id FROM request_tags WHERE request_id = ?`, id)
		if err != nil {
			return fmt.Errorf("listing tag memberships of request %d: %w", id, err)
		}
		favouriteIDs, err := selectIDs(tx, `SELECT id FROM favourite_items WHERE http_request_id = ?`, id)
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

// deleteFavouriteCollection removes a favourite collection together with
// its memberships and appearance. Undo puts all of it back as it was.
func deleteFavouriteCollection(s *FavouriteService) *commands.Command {
	return &commands.Command{
		Name:     FavouriteDeleteCollectionCommandName,
		Label:    "Delete Favourite",
		Undoable: true,
		Apply: func(tx *sql.Tx, rec *commands.Recorder, input json.RawMessage) (any, error) {
			var payload struct {
				ID int64 `json:"id"`
			}
			if err := json.Unmarshal(input, &payload); err != nil {
				return nil, err
			}
			if payload.ID == 0 {
				return nil, errors.New("favourite collection id is required")
			}

			itemIDs, err := selectIDs(tx,
				`SELECT id FROM favourite_items WHERE favourite_collection_id = ?`, payload.ID)
			if err != nil {
				return nil, fmt.Errorf("listing favourite items: %w", err)
			}
			appearanceIDs, err := selectIDs(tx,
				`SELECT id FROM favourite_appearances WHERE favourite_collection_id = ?`, payload.ID)
			if err != nil {
				return nil, fmt.Errorf("listing favourite appearance: %w", err)
			}

			// Parent table first: rows are put back in save order.
			if err := rec.SaveRows("favourite_collections", payload.ID); err != nil {
				return nil, err
			}
			if err := rec.SaveRows("favourite_items", itemIDs...); err != nil {
				return nil, err
			}
			if err := rec.SaveRows("favourite_appearances", appearanceIDs...); err != nil {
				return nil, err
			}

			if _, err := tx.Exec(
				`DELETE FROM favourite_collections WHERE id = ?`, payload.ID,
			); err != nil {
				return nil, fmt.Errorf("deleting favourite collection: %w", err)
			}

			rec.Touched("favourite_collection", "delete", payload.ID)
			return nil, nil
		},
	}
}

// deleteEnvironment removes an environment together with its variables;
// the default environment is refused. Undo puts all of it back as it was.
func deleteEnvironment(s *EnvironmentService) *commands.Command {
	return &commands.Command{
		Name:     EnvironmentDeleteCommandName,
		Label:    "Delete Environment",
		Undoable: true,
		Apply: func(tx *sql.Tx, rec *commands.Recorder, input json.RawMessage) (any, error) {
			var payload struct {
				ID int64 `json:"id"`
			}
			if err := json.Unmarshal(input, &payload); err != nil {
				return nil, err
			}
			if payload.ID == 0 {
				return nil, errors.New("environment id is required")
			}

			var name string
			if err := tx.QueryRow(
				`SELECT name FROM environments WHERE id = ?`, payload.ID,
			).Scan(&name); err != nil {
				if err == sql.ErrNoRows {
					return nil, errors.New("environment not found")
				}
				return nil, fmt.Errorf("loading environment: %w", err)
			}
			if strings.EqualFold(name, "Default") {
				return nil, errors.New("cannot delete the default environment")
			}

			variableIDs, err := selectIDs(tx,
				`SELECT id FROM environment_variables WHERE environment_id = ?`, payload.ID)
			if err != nil {
				return nil, fmt.Errorf("listing environment variables: %w", err)
			}

			// Parent table first: rows are put back in save order.
			if err := rec.SaveRows("environments", payload.ID); err != nil {
				return nil, err
			}
			if err := rec.SaveRows("environment_variables", variableIDs...); err != nil {
				return nil, err
			}

			if _, err := tx.Exec(
				`DELETE FROM environments WHERE id = ?`, payload.ID,
			); err != nil {
				return nil, fmt.Errorf("deleting environment: %w", err)
			}

			rec.Touched("environment", "delete", payload.ID)
			return nil, nil
		},
	}
}

// deleteVariable removes one variable. Undo puts the exact key/value row
// back as it was.
func deleteVariable(s *EnvironmentVariableService) *commands.Command {
	return &commands.Command{
		Name:     EnvironmentVariableDeleteCommandName,
		Label:    "Delete Variable",
		Undoable: true,
		Apply: func(tx *sql.Tx, rec *commands.Recorder, input json.RawMessage) (any, error) {
			var payload struct {
				ID int64 `json:"id"`
			}
			if err := json.Unmarshal(input, &payload); err != nil {
				return nil, err
			}
			if payload.ID == 0 {
				return nil, errors.New("variable id is required")
			}

			if err := rec.SaveRows("environment_variables", payload.ID); err != nil {
				return nil, err
			}

			result, err := tx.Exec(
				`DELETE FROM environment_variables WHERE id = ?`, payload.ID,
			)
			if err != nil {
				return nil, fmt.Errorf("deleting variable: %w", err)
			}
			rows, err := result.RowsAffected()
			if err == nil && rows == 0 {
				return nil, errors.New("variable not found")
			}

			rec.Touched("environment_variable", "delete", payload.ID)
			return nil, nil
		},
	}
}

// selectIDs returns the ids produced by a single-column id query, inside
// the command's transaction. Used to list the rows that will die with a
// delete, so their copies are saved too.
func selectIDs(tx *sql.Tx, query string, args ...any) ([]int64, error) {
	rows, err := tx.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ---------------------------------------------------------------------------
// The Wails service: the frontend's door to the history.
// ---------------------------------------------------------------------------

// CommandService exposes the command history to the frontend: running
// commands by name, undo/redo, the status for the undo menu, the step list
// for the inspector panel, and the list of known commands for future
// external callers.
type CommandService struct {
	history *commands.History
}

// NewCommandService returns a CommandService backed by the given history.
func NewCommandService(history *commands.History) *CommandService {
	return &CommandService{history: history}
}

// CommandExecuteResult carries a command's marshalled result.
type CommandExecuteResult struct {
	Result json.RawMessage `json:"result"`
}

// Execute runs a named command with a JSON payload and returns the
// marshalled result. This is the entry point future external callers share
// with the UI.
func (s *CommandService) Execute(name string, payload string) (CommandExecuteResult, error) {
	out, err := s.history.Run(name, json.RawMessage(payload))
	if err != nil {
		return CommandExecuteResult{}, err
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return CommandExecuteResult{}, err
	}
	return CommandExecuteResult{Result: raw}, nil
}

// Undo puts the newest remembered step back and reports what happened.
func (s *CommandService) Undo() (commands.UndoResult, error) {
	return s.history.Undo()
}

// Redo puts the newest undone step back again.
func (s *CommandService) Redo() (commands.UndoResult, error) {
	return s.history.Redo()
}

// History returns the current summary for the undo menu.
func (s *CommandService) History() commands.Status {
	return s.history.Status()
}

// Stack lists every remembered step, oldest first, for the inspector
// panel.
func (s *CommandService) Stack() []commands.StepInfo {
	return s.history.Steps()
}

// ListCommands returns the known commands, for callers that ask "what can
// this app do?".
func (s *CommandService) ListCommands() []commands.CommandInfo {
	return s.history.KnownCommands()
}
