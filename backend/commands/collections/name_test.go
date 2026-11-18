package collections

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"snap-rq/backend/database"
	"snap-rq/backend/models"
)

func TestCreateAndRenameRejectDuplicateNames(t *testing.T) {
	db, err := database.OpenPath(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("beginning tx: %v", err)
	}
	defer tx.Rollback()

	// OpenPath seeds project 1 with a collection called "my stuff".
	dup, _ := json.Marshal(map[string]any{"project_id": 1, "name": "My Stuff"})
	if _, err := NewCreate(tx).Run(dup); err == nil {
		t.Fatal("creating a collection with an existing name (any case) should fail")
	}

	ok, _ := json.Marshal(map[string]any{"project_id": 1, "name": "billing"})
	res, err := NewCreate(tx).Run(ok)
	if err != nil {
		t.Fatalf("creating a collection with a new name: %v", err)
	}
	created := res.(models.Collection)

	clash, _ := json.Marshal(map[string]any{"id": created.ID, "project_id": 1, "name": "MY STUFF"})
	if _, err := NewUpdate(tx).Run(clash); err == nil {
		t.Fatal("renaming onto an existing name should fail")
	}

	same, _ := json.Marshal(map[string]any{"id": created.ID, "project_id": 1, "name": "Billing"})
	if _, err := NewUpdate(tx).Run(same); err != nil {
		t.Fatalf("changing only the case of its own name should work: %v", err)
	}
}
