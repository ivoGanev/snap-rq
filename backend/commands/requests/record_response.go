package requests

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"snap-rq/backend/commands"
)

// record_response.go — note a send's outcome on the request row: its latest
// status code and newest response id. The send flow owns these columns;
// user-edit commands never write them.

type RecordResponse struct{ tx *sql.Tx }

func NewRecordResponse(tx *sql.Tx) commands.Command { return &RecordResponse{tx: tx} }

func (RecordResponse) Info() commands.Info {
	return commands.Info{FunctionID: "request.record_response", Label: "Record Response"}
}

func (c RecordResponse) Run(input json.RawMessage) (any, error) {
	var in struct {
		RequestID  int64 `json:"request_id"`
		StatusCode int   `json:"status_code"`
		ResponseID int64 `json:"response_id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if in.RequestID == 0 {
		return nil, fmt.Errorf("request id is required")
	}
	if _, err := c.tx.Exec(
		`UPDATE http_requests SET status_code = ?, response_id = ? WHERE id = ?`,
		in.StatusCode, in.ResponseID, in.RequestID,
	); err != nil {
		return nil, fmt.Errorf("recording response for request %d: %w", in.RequestID, err)
	}
	return nil, nil
}
