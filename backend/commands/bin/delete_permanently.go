package bin

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"snap-rq/backend/commands"
)

// delete_permanently.go — destroy binned requests forever. Not undoable.
// Undo steps for the destroyed requests fail later and are skipped: the
// data they would restore is gone for good.

type DeletePermanently struct{ tx *sql.Tx }

func NewDeletePermanently(tx *sql.Tx) commands.Command { return &DeletePermanently{tx: tx} }

func (DeletePermanently) Info() commands.Info {
	return commands.Info{FunctionID: "bin.delete_permanently", Label: "Delete Forever"}
}

func (c DeletePermanently) Run(input json.RawMessage) (any, error) {
	var in struct {
		BinIDs []int64 `json:"bin_ids"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if len(in.BinIDs) == 0 {
		return nil, nil
	}

	originalIDs := make([]int64, 0, len(in.BinIDs))
	for _, binID := range in.BinIDs {
		var originalID int64
		if err := c.tx.QueryRow(
			`SELECT original_request_id FROM binned_requests WHERE id = ?`, binID,
		).Scan(&originalID); err != nil {
			return nil, fmt.Errorf("loading binned request %d: %w", binID, err)
		}
		originalIDs = append(originalIDs, originalID)
	}

	// drop undo steps that would resurrect the destroyed requests
	for _, originalID := range originalIDs {
		if _, err := c.tx.Exec(
			`DELETE FROM undo_log WHERE command = 'request.delete' AND input LIKE ?`,
			fmt.Sprintf(`%%"ids":[%d]%%`, originalID),
		); err != nil {
			return nil, err
		}
	}

	query := `DELETE FROM binned_requests WHERE id IN (` + placeholders(len(in.BinIDs)) + `)`
	args := make([]any, len(in.BinIDs))
	for i, id := range in.BinIDs {
		args[i] = id
	}
	if _, err := c.tx.Exec(query, args...); err != nil {
		return nil, fmt.Errorf("deleting binned requests: %w", err)
	}
	return map[string]any{"deleted": len(in.BinIDs)}, nil
}

func placeholders(n int) string {
	out := ""
	for i := 0; i < n; i++ {
		if i > 0 {
			out += ","
		}
		out += "?"
	}
	return out
}
