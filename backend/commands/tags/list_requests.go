package tags

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// list_requests.go — the requests in one tag, in the user's order.

type ListRequests struct{ tx *sql.Tx }

func NewListRequests(tx *sql.Tx) commands.Command { return &ListRequests{tx: tx} }

func (ListRequests) Info() commands.Info {
	return commands.Info{FunctionID: "tag.list_requests", Label: "List Tag Requests", ReadOnly: true}
}

func (c ListRequests) Run(input json.RawMessage) (any, error) {
	var in struct {
		ProjectID int64  `json:"project_id"`
		TagName   string `json:"tag_name"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}

	rows, err := c.tx.Query(`
		SELECT hr.id, hr.collection_id, c.project_id, hr.name, hr.url, hr.method, hr.status_code,
		       hr.response_id, hr.user_sort_order, hr.user_color
		FROM http_requests hr
		JOIN collections c ON c.id = hr.collection_id
		JOIN request_tags rt ON rt.request_id = hr.id
		JOIN tags t ON t.id = rt.tag_id
		WHERE t.project_id = ? AND t.name = ?
		ORDER BY rt.user_sort_order, hr.name`, in.ProjectID, in.TagName)
	if err != nil {
		return nil, fmt.Errorf("listing tag requests: %w", err)
	}
	defer rows.Close()

	var summaries []models.HttpRequestSummary
	for rows.Next() {
		var s models.HttpRequestSummary
		if err := rows.Scan(&s.ID, &s.CollectionID, &s.ProjectID, &s.Name, &s.URL, &s.Method,
			&s.StatusCode, &s.ResponseID, &s.UserSortOrder, &s.UserColor); err != nil {
			return nil, err
		}
		summaries = append(summaries, s)
	}
	return summaries, rows.Err()
}

var _ = strings.TrimSpace
