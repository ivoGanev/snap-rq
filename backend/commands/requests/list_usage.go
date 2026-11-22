package requests

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"snap-rq/backend/commands"
)

// list_usage.go — how much a project's requests are used lately (see
// usage.go): the score faded to now and its level, for requests with any.

type ListUsage struct{ tx *sql.Tx }

func NewListUsage(tx *sql.Tx) commands.Command { return &ListUsage{tx: tx} }

func (ListUsage) Info() commands.Info {
	return commands.Info{FunctionID: "request.list_usage", Label: "List Request Usage", ReadOnly: true}
}

// RequestUsage is one request's recent usage.
type RequestUsage struct {
	ID    int64   `json:"id"`
	Score float64 `json:"score"`
	Level string  `json:"level"` // "high" | "medium" | "low"
}

func (c ListUsage) Run(input json.RawMessage) (any, error) {
	var in struct {
		ProjectID int64 `json:"project_id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	rows, err := c.tx.Query(`
		SELECT hr.id, hr.use_score, hr.last_used_at FROM http_requests hr
		JOIN collections c ON c.id = hr.collection_id
		WHERE c.project_id = ? AND hr.use_score > 0`, in.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("listing usage: %w", err)
	}
	defer rows.Close()

	now := time.Now()
	usage := []RequestUsage{}
	for rows.Next() {
		var id, lastUsed int64
		var score float64
		if err := rows.Scan(&id, &score, &lastUsed); err != nil {
			return nil, err
		}
		faded := fadedScore(score, lastUsed, now)
		if level := usageLevel(faded); level != "" {
			usage = append(usage, RequestUsage{ID: id, Score: faded, Level: level})
		}
	}
	return usage, rows.Err()
}
