package services

import (
	"testing"

	"snap-rq/backend/models"
)

func TestMigrateIconIds(t *testing.T) {
	db := newTestDB(t)
	runMigrations(t, db)

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS collection_appearances (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			collection_id INTEGER NOT NULL,
			appearance_type TEXT NOT NULL,
			appearance_value TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS favourite_appearances (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			favourite_collection_id INTEGER NOT NULL,
			appearance_type TEXT NOT NULL,
			appearance_value TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS tag_appearances (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tag_id INTEGER NOT NULL,
			appearance_type TEXT NOT NULL,
			appearance_value TEXT NOT NULL
		);
		INSERT INTO collection_appearances (collection_id, appearance_type, appearance_value) VALUES (1, 'icon', 'wood'), (2, 'icon', 'diamond');
		INSERT INTO favourite_appearances (favourite_collection_id, appearance_type, appearance_value) VALUES (1, 'icon', 'wood');
		INSERT INTO tag_appearances (tag_id, appearance_type, appearance_value) VALUES (1, 'icon', 'wood'), (2, 'color', '#ff0000');
	`); err != nil {
		t.Fatalf("setting up appearance schema: %v", err)
	}

	svc := NewIconService(db)
	if err := svc.MigrateIconIds([]models.IconReplacement{{OldID: "wood", NewID: "wood_v2"}}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	read := func(query string) string {
		var v string
		if err := db.QueryRow(query).Scan(&v); err != nil {
			t.Fatalf("reading %s: %v", query, err)
		}
		return v
	}

	if v := read("SELECT appearance_value FROM collection_appearances WHERE id = 1"); v != "wood_v2" {
		t.Fatalf("collection icon not migrated: %s", v)
	}
	if v := read("SELECT appearance_value FROM collection_appearances WHERE id = 2"); v != "diamond" {
		t.Fatalf("unrelated icon changed: %s", v)
	}
	if v := read("SELECT appearance_value FROM favourite_appearances WHERE id = 1"); v != "wood_v2" {
		t.Fatalf("favourite icon not migrated: %s", v)
	}
	if v := read("SELECT appearance_value FROM tag_appearances WHERE id = 1"); v != "wood_v2" {
		t.Fatalf("tag icon not migrated: %s", v)
	}
	if v := read("SELECT appearance_value FROM tag_appearances WHERE id = 2"); v != "#ff0000" {
		t.Fatalf("colour row changed: %s", v)
	}

	// Idempotent: a second run changes nothing.
	if err := svc.MigrateIconIds([]models.IconReplacement{{OldID: "wood", NewID: "wood_v2"}}); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if v := read("SELECT appearance_value FROM collection_appearances WHERE id = 1"); v != "wood_v2" {
		t.Fatalf("icon changed on second run: %s", v)
	}
}
