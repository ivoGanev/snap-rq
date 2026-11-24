package responses

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"snap-rq/backend/database"
)

func TestClearDeletesOneRequestsHistoryAndKeepsItsUsage(t *testing.T) {
	db, err := database.OpenPath(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	var collection int64
	if err := tx.QueryRow(`SELECT id FROM collections LIMIT 1`).Scan(&collection); err != nil {
		t.Fatal(err)
	}
	request := func(name string) int64 {
		res, err := tx.Exec(`INSERT INTO http_requests (collection_id, name, url, method, body_type, last_used_at, use_score)
			VALUES (?, ?, '', 'GET', 'json', 777, 3.5)`, collection, name)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	respond := func(requestID int64, status int) int64 {
		res, err := tx.Exec(`INSERT INTO responses (request_id, status_code, body) VALUES (?, ?, 'x')`, requestID, status)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		if _, err := tx.Exec(`UPDATE http_requests SET status_code = ?, response_id = ? WHERE id = ?`, status, id, requestID); err != nil {
			t.Fatal(err)
		}
		return id
	}
	cleared, kept := request("cleared"), request("kept")
	respond(cleared, 200)
	respond(cleared, 500)
	keptResponse := respond(kept, 201)

	in, _ := json.Marshal(map[string]any{"request_id": cleared})
	if _, err := NewClear(tx).Run(in); err != nil {
		t.Fatal(err)
	}

	count := func(requestID int64) int {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM responses WHERE request_id = ?`, requestID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(cleared); n != 0 {
		t.Errorf("%d responses left on the cleared request", n)
	}
	if n := count(kept); n != 1 {
		t.Errorf("the other request has %d responses, want its 1 untouched", n)
	}

	var status int
	var responseID, lastUsed int64
	var score float64
	if err := tx.QueryRow(`SELECT status_code, response_id, last_used_at, use_score FROM http_requests WHERE id = ?`, cleared).
		Scan(&status, &responseID, &lastUsed, &score); err != nil {
		t.Fatal(err)
	}
	if status != 0 || responseID != 0 {
		t.Errorf("status %d, response_id %d: both should reset with the history", status, responseID)
	}
	if lastUsed != 777 || score != 3.5 {
		t.Errorf("last_used_at %d, use_score %v: usage records sends and must stay", lastUsed, score)
	}

	if err := tx.QueryRow(`SELECT status_code, response_id FROM http_requests WHERE id = ?`, kept).Scan(&status, &responseID); err != nil {
		t.Fatal(err)
	}
	if status != 201 || responseID != keptResponse {
		t.Errorf("the other request's status changed to %d / %d", status, responseID)
	}
}
