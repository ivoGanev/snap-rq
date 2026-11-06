// Package bin holds the trash commands. bin.restore is the user-facing
// trash button (recreates requests from the trash snapshots with new ids)
// and is itself undoable: undo deletes what it made and puts the trash
// rows back.
package bin

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// list.go — a project's binned requests, newest first.

type List struct{ tx *sql.Tx }

func NewList(tx *sql.Tx) commands.Command { return &List{tx: tx} }

func (List) Info() commands.Info {
	return commands.Info{FunctionID: "bin.list", Label: "List Binned Requests", ReadOnly: true}
}

func (c List) Run(input json.RawMessage) (any, error) {
	var in struct {
		ProjectID int64 `json:"project_id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	rows, err := c.tx.Query(`
		SELECT id, original_request_id, project_id, collection_id, original_collection_id,
		       original_collection_name, name, url, method, status_code, response_id, deleted_at
		FROM binned_requests WHERE project_id = ?
		ORDER BY deleted_at DESC, id DESC`, in.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("listing binned requests: %w", err)
	}
	defer rows.Close()

	var summaries []models.HttpBinnedRequestSummary
	for rows.Next() {
		var s models.HttpBinnedRequestSummary
		if err := rows.Scan(&s.ID, &s.OriginalRequestID, &s.ProjectID, &s.CollectionID,
			&s.OriginalCollectionID, &s.OriginalCollectionName,
			&s.Name, &s.URL, &s.Method, &s.StatusCode, &s.ResponseID, &s.DeletedAt); err != nil {
			return nil, fmt.Errorf("reading a binned request: %w", err)
		}
		summaries = append(summaries, s)
	}
	return summaries, rows.Err()
}
