// Package groups holds the request group commands, one operation per file.
package groups

import (
	"database/sql"
	"fmt"
	"strings"

	"snap-rq/backend/models"
)

func queryGroup(tx *sql.Tx, id int64) (models.RequestGroup, error) {
	var g models.RequestGroup
	err := tx.QueryRow(`SELECT id, collection_id, name, color FROM request_groups WHERE id = ?`, id).
		Scan(&g.ID, &g.CollectionID, &g.Name, &g.Color)
	if err != nil {
		return g, fmt.Errorf("loading group %d: %w", id, err)
	}
	return g, nil
}

// membership is one request's group before a change (nil = none).
type membership struct {
	ID      int64  `json:"id"`
	GroupID *int64 `json:"group_id"`
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func int64Args(ids []int64) []any {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return args
}

func readMemberships(tx *sql.Tx, requestIDs []int64) ([]membership, error) {
	if len(requestIDs) == 0 {
		return nil, nil
	}
	rows, err := tx.Query(
		`SELECT id, group_id FROM http_requests WHERE id IN (`+placeholders(len(requestIDs))+`)`,
		int64Args(requestIDs)...,
	)
	if err != nil {
		return nil, fmt.Errorf("reading request groups: %w", err)
	}
	defer rows.Close()
	var out []membership
	for rows.Next() {
		var m membership
		if err := rows.Scan(&m.ID, &m.GroupID); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func restoreMemberships(tx *sql.Tx, ms []membership) error {
	for _, m := range ms {
		if _, err := tx.Exec(`UPDATE http_requests SET group_id = ? WHERE id = ?`, m.GroupID, m.ID); err != nil {
			return err
		}
	}
	return nil
}

// assign puts the requests in the group (nil = no group). Only requests of
// the group's own collection can join it.
func assign(tx *sql.Tx, groupID *int64, requestIDs []int64) error {
	if len(requestIDs) == 0 {
		return nil
	}
	args := append([]any{groupID}, int64Args(requestIDs)...)
	query := `UPDATE http_requests SET group_id = ? WHERE id IN (` + placeholders(len(requestIDs)) + `)`
	if groupID != nil {
		query += ` AND collection_id = (SELECT collection_id FROM request_groups WHERE id = ?)`
		args = append(args, *groupID)
	}
	_, err := tx.Exec(query, args...)
	return err
}
