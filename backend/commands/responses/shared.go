// Package responses holds the send flow's persistence commands. Response
// history is append-only and capped — never undoable.
package responses

import (
	"database/sql"
	"fmt"
	"time"

	"snap-rq/backend/models"
)

// insertResponse stores one response row and trims the history to the
// newest ten entries per request.
func insertResponse(tx *sql.Tx, resp models.HttpResponse) (models.HttpResponse, error) {
	if resp.CreatedAt == "" {
		resp.CreatedAt = time.Now().UTC().Format("2006-01-02 15:04:05")
	}
	result, err := tx.Exec(`
		INSERT INTO responses (request_id, headers, status_code, body, created_at, duration_ms)
		VALUES (?, ?, ?, ?, ?, ?)`,
		resp.RequestID, resp.Headers, resp.StatusCode, resp.Body, resp.CreatedAt, resp.DurationMs)
	if err != nil {
		return models.HttpResponse{}, fmt.Errorf("saving response: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return models.HttpResponse{}, err
	}
	resp.ID = id

	if _, err := tx.Exec(`DELETE FROM responses WHERE request_id = ? AND id NOT IN (
		SELECT id FROM responses WHERE request_id = ? ORDER BY id DESC LIMIT 10)`,
		resp.RequestID, resp.RequestID); err != nil {
		return models.HttpResponse{}, fmt.Errorf("trimming response history: %w", err)
	}
	return resp, nil
}
