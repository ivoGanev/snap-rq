package responses

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// update.go — rewrite a stored response row (the in-flight duration
// updates and the final body of a single send).

type Update struct{ tx *sql.Tx }

func NewUpdate(tx *sql.Tx) commands.Command { return &Update{tx: tx} }

func (Update) Info() commands.Info {
	return commands.Info{FunctionID: "response.update", Label: "Update Response"}
}

func (c Update) Run(input json.RawMessage) (any, error) {
	var resp models.HttpResponse
	if err := json.Unmarshal(input, &resp); err != nil {
		return nil, err
	}
	if resp.ID == 0 {
		return nil, fmt.Errorf("response id is required")
	}
	if _, err := c.tx.Exec(
		`UPDATE responses SET request_id = ?, headers = ?, status_code = ?, body = ?,
		     created_at = ?, duration_ms = ? WHERE id = ?`,
		resp.RequestID, resp.Headers, resp.StatusCode, resp.Body,
		resp.CreatedAt, resp.DurationMs, resp.ID,
	); err != nil {
		return nil, fmt.Errorf("updating response: %w", err)
	}
	return resp, nil
}
