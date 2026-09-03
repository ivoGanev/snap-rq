package database

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigrateCollectionAppearancesFromLegacyIconIDColumnOnly(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening in-memory db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`
		CREATE TABLE collections (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			icon_id TEXT NOT NULL DEFAULT ''
		);
		INSERT INTO collections (project_id, name, icon_id) VALUES (1, 'with-icon', 'heart');
		INSERT INTO collections (project_id, name, icon_id) VALUES (2, 'no-icon', '');
	`); err != nil {
		t.Fatalf("setting up legacy schema: %v", err)
	}

	if err := migrate(db); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}

	rows, err := db.Query("SELECT collection_id, appearance_type, appearance_value FROM collection_appearances ORDER BY collection_id")
	if err != nil {
		t.Fatalf("reading appearances: %v", err)
	}
	defer rows.Close()

	var appearances []struct {
		id    int64
		typ   string
		value string
	}
	for rows.Next() {
		var a struct {
			id    int64
			typ   string
			value string
		}
		if err := rows.Scan(&a.id, &a.typ, &a.value); err != nil {
			t.Fatalf("scanning appearance: %v", err)
		}
		appearances = append(appearances, a)
	}

	if len(appearances) != 2 {
		t.Fatalf("expected 2 appearances, got %d", len(appearances))
	}

	if appearances[0].typ != "icon" || appearances[0].value != "heart" {
		t.Errorf("expected icon/heart for first collection, got %s/%s", appearances[0].typ, appearances[0].value)
	}
	if appearances[1].typ != "icon" || appearances[1].value != "default" {
		t.Errorf("expected icon/default for second collection, got %s/%s", appearances[1].typ, appearances[1].value)
	}
}
