package responses

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// save_bulk.go — the durable write of a multi-request send: every response
// in one transaction, each originating request pointed at its newest
// status. The send flow owns these two columns; user-edit commands never
// write them, so an undo of a user edit can never clobber a send.

type SaveBulk struct{ tx *sql.Tx }

func NewSaveBulk(tx *sql.Tx) commands.Command { return &SaveBulk{tx: tx} }

func (SaveBulk) Info() commands.Info {
	return commands.Info{FunctionID: "response.save_bulk", Label: "Save Responses"}
}

func (c SaveBulk) Run(input json.RawMessage) (any, error) {
	var in struct {
		Responses []models.HttpResponse `json:"responses"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if len(in.Responses) == 0 {
		return nil, nil
	}

	saved := make([]models.HttpResponse, 0, len(in.Responses))
	for _, resp := range in.Responses {
		stored, err := insertResponse(c.tx, resp)
		if err != nil {
			return nil, fmt.Errorf("saving response for request %d: %w", resp.RequestID, err)
		}
		if _, err := c.tx.Exec(
			`UPDATE http_requests SET status_code = ?, response_id = ? WHERE id = ?`,
			stored.StatusCode, stored.ID, resp.RequestID,
		); err != nil {
			return nil, err
		}
		saved = append(saved, stored)
	}
	return saved, nil
}
