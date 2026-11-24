package responses

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"snap-rq/backend/commands"
)

// clear.go — delete a request's whole response history. Not undoable: the
// undo log would have to keep every body. The request's status and
// response_id point at its latest response, so they reset too; its usage
// (last_used_at, use_score) records sends and stays.

type Clear struct{ tx *sql.Tx }

func NewClear(tx *sql.Tx) commands.Command { return &Clear{tx: tx} }

func (Clear) Info() commands.Info {
	return commands.Info{FunctionID: "response.clear_for_request", Label: "Clear Response History"}
}

func (c Clear) Run(input json.RawMessage) (any, error) {
	var in struct {
		RequestID int64 `json:"request_id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if in.RequestID == 0 {
		return nil, errors.New("request_id is required")
	}
	res, err := c.tx.Exec(`DELETE FROM responses WHERE request_id = ?`, in.RequestID)
	if err != nil {
		return nil, fmt.Errorf("clearing responses: %w", err)
	}
	if _, err := c.tx.Exec(
		`UPDATE http_requests SET status_code = 0, response_id = 0 WHERE id = ?`, in.RequestID,
	); err != nil {
		return nil, fmt.Errorf("resetting the request's status: %w", err)
	}
	deleted, _ := res.RowsAffected()
	return map[string]any{"deleted": deleted}, nil
}

func (Clear) Changes(input, _ json.RawMessage) []commands.RowChange {
	return []commands.RowChange{{Component: "request", IDs: commands.IDsOf(input, "request_id")}}
}
