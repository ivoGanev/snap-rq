package responses

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// list.go — a request's response history, newest first.

type List struct{ tx *sql.Tx }

func NewList(tx *sql.Tx) commands.Command { return &List{tx: tx} }

func (List) Info() commands.Info {
	return commands.Info{FunctionID: "response.list_for_request", Label: "List Responses", ReadOnly: true}
}

func (c List) Run(input json.RawMessage) (any, error) {
	var in struct {
		RequestID int64 `json:"request_id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	rows, err := c.tx.Query(
		`SELECT id, request_id, COALESCE(headers, ''), status_code, COALESCE(body, ''), created_at, duration_ms
		 FROM responses WHERE request_id = ? ORDER BY id DESC`, in.RequestID)
	if err != nil {
		return nil, fmt.Errorf("listing responses: %w", err)
	}
	defer rows.Close()

	var responses []models.HttpResponse
	for rows.Next() {
		var resp models.HttpResponse
		if err := rows.Scan(&resp.ID, &resp.RequestID, &resp.Headers, &resp.StatusCode,
			&resp.Body, &resp.CreatedAt, &resp.DurationMs); err != nil {
			return nil, fmt.Errorf("reading a response: %w", err)
		}
		responses = append(responses, resp)
	}
	return responses, rows.Err()
}
