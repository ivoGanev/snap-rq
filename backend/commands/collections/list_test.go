package collections

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"snap-rq/backend/database"
	"snap-rq/backend/models"
)

func TestListGivesEachCollectionItsLatestSend(t *testing.T) {
	db, err := database.OpenPath(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	in, _ := json.Marshal(map[string]any{"project_id": 1, "name": "never sent"})
	res, err := NewCreate(tx).Run(in)
	if err != nil {
		t.Fatal(err)
	}
	idle := res.(models.Collection).ID
	var used int64
	if err := tx.QueryRow(`SELECT id FROM collections WHERE project_id = 1 AND id != ? LIMIT 1`, idle).Scan(&used); err != nil {
		t.Fatal(err)
	}
	for _, sent := range []int64{0, 300, 200} {
		if _, err := tx.Exec(`INSERT INTO http_requests (collection_id, name, url, method, body_type, last_used_at)
			VALUES (?, 'r', '', 'GET', 'json', ?)`, used, sent); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO http_requests (collection_id, name, url, method, body_type)
		VALUES (?, 'r', '', 'GET', 'json')`, idle); err != nil {
		t.Fatal(err)
	}

	list, _ := json.Marshal(map[string]any{"project_id": 1})
	out, err := NewList(tx).Run(list)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range out.([]models.Collection) {
		switch c.ID {
		case used:
			if c.LastUsedAt != 300 {
				t.Errorf("used collection's last_used_at = %d, want its latest send, 300", c.LastUsedAt)
			}
		case idle:
			if c.LastUsedAt != 0 {
				t.Errorf("a collection with nothing sent has last_used_at %d, want 0", c.LastUsedAt)
			}
		}
	}
}
