package favourites

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// list_requests.go — the requests in one favourite collection, in the
// user's order.

type ListRequests struct{ tx *sql.Tx }

func NewListRequests(tx *sql.Tx) commands.Command { return &ListRequests{tx: tx} }

func (ListRequests) Info() commands.Info {
	return commands.Info{FunctionID: "favourite.list_requests", Label: "List Favourite Requests", ReadOnly: true}
}

func (c ListRequests) Run(input json.RawMessage) (any, error) {
	var in struct {
		FavouriteCollectionID int64 `json:"favourite_collection_id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	rows, err := c.tx.Query(`
		SELECT r.id, r.collection_id, c.project_id, r.name, r.url, r.method, r.body,
		       r.body_type, r.request_headers, r.status_code, r.response_id,
		       fi.user_sort_order, fi.user_color, r.last_used_at
		FROM http_requests r
		JOIN collections c ON c.id = r.collection_id
		JOIN favourite_items fi ON fi.http_request_id = r.id
		WHERE fi.favourite_collection_id = ?
		ORDER BY fi.user_sort_order, r.name`, in.FavouriteCollectionID)
	if err != nil {
		return nil, fmt.Errorf("listing favourite requests: %w", err)
	}
	defer rows.Close()

	var requests []models.HttpRequest
	for rows.Next() {
		var req models.HttpRequest
		if err := rows.Scan(&req.ID, &req.CollectionID, &req.ProjectID, &req.Name, &req.URL,
			&req.Method, &req.Body, &req.BodyType, &req.RequestHeaders, &req.StatusCode,
			&req.ResponseID, &req.UserSortOrder, &req.UserColor, &req.LastUsedAt); err != nil {
			return nil, fmt.Errorf("reading a favourite request: %w", err)
		}
		requests = append(requests, req)
	}
	return requests, rows.Err()
}
