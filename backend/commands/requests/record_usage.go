package requests

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"snap-rq/backend/commands"
)

// record_usage.go — mark requests as most recently used. Telemetry: not
// undoable, and no change report (the caller updates its own list).

type RecordUsage struct{ tx *sql.Tx }

func NewRecordUsage(tx *sql.Tx) commands.Command { return &RecordUsage{tx: tx} }

func (RecordUsage) Info() commands.Info {
	return commands.Info{FunctionID: "request.record_usage", Label: "Record Usage"}
}

func (c RecordUsage) Run(input json.RawMessage) (any, error) {
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if len(in.IDs) == 0 {
		return nil, nil
	}

	usedAt := time.Now().UTC().UnixNano()
	for _, id := range in.IDs {
		if _, err := c.tx.Exec(
			`UPDATE http_requests SET last_used_at = ? WHERE id = ?`, usedAt, id,
		); err != nil {
			return nil, fmt.Errorf("recording usage for request %d: %w", id, err)
		}
	}
	return nil, nil
}
