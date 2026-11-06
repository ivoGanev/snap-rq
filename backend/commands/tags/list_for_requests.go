package tags

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"snap-rq/backend/commands"
)

//  — the tags of many requests at once, as a map.

type ListForRequests struct{ tx *sql.Tx }

func NewListForRequests(tx *sql.Tx) commands.Command { return &ListForRequests{tx: tx} }

func (ListForRequests) Info() commands.Info {
	return commands.Info{FunctionID: "tag.list_for_requests", Label: "List Tags For Requests", ReadOnly: true}
}

func (c ListForRequests) Run(input json.RawMessage) (any, error) {
	var in struct {
		RequestIDs []int64 `json:"request_ids"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if len(in.RequestIDs) == 0 {
		return map[string][]string{}, nil
	}

	placeholders := ""
	args := make([]any, len(in.RequestIDs))
	for i, id := range in.RequestIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args[i] = id
	}
	rows, err := c.tx.Query(`
		SELECT rt.request_id, t.name FROM request_tags rt
		JOIN tags t ON t.id = rt.tag_id
		WHERE rt.request_id IN (`+placeholders+`) ORDER BY t.name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := map[string][]string{}
	for rows.Next() {
		var requestID int64
		var name string
		if err := rows.Scan(&requestID, &name); err != nil {
			return nil, err
		}
		key := fmt.Sprintf("%d", requestID)
		result[key] = append(result[key], name)
	}
	return result, rows.Err()
}
