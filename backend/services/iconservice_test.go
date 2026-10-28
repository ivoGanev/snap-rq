package services

import (
	"testing"
)

func TestCleanupIconReferences(t *testing.T) {
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
		INSERT INTO collection_appearances (collection_id, appearance_type, appearance_value) VALUES (1, 'icon', 'ico_7k2f9'), (2, 'icon', 'ico_2b8e3'), (3, 'color', '#ff0000');
		INSERT INTO favourite_appearances (favourite_collection_id, appearance_type, appearance_value) VALUES (1, 'icon', 'ico_2b8e3');
		INSERT INTO tag_appearances (tag_id, appearance_type, appearance_value) VALUES (1, 'icon', 'ico_2b8e3'), (2, 'color', '#00ff00');
	`); err != nil {
		t.Fatalf("setting up appearance schema: %v", err)
	}

	validIDs := []string{"ico_1a9d4", "ico_3c7f2", "ico_7k2f9", "ico_4d6a1", "ico_5e5b0", "ico_6f4c9"}
	svc := NewIconService(db)
	if err := svc.CleanupIconReferences(validIDs, "ico_1a9d4"); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	read := func(query string) string {
		var v string
		if err := db.QueryRow(query).Scan(&v); err != nil {
			t.Fatalf("reading %s: %v", query, err)
		}
		return v
	}

	if v := read("SELECT appearance_value FROM collection_appearances WHERE id = 1"); v != "ico_7k2f9" {
		t.Fatalf("valid icon changed: %s", v)
	}
	if v := read("SELECT appearance_value FROM collection_appearances WHERE id = 2"); v != "ico_1a9d4" {
		t.Fatalf("removed icon not reset: %s", v)
	}
	if v := read("SELECT appearance_value FROM collection_appearances WHERE id = 3"); v != "#ff0000" {
		t.Fatalf("colour row changed: %s", v)
	}
	if v := read("SELECT appearance_value FROM favourite_appearances WHERE id = 1"); v != "ico_1a9d4" {
		t.Fatalf("favourite removed icon not reset: %s", v)
	}
	if v := read("SELECT appearance_value FROM tag_appearances WHERE id = 1"); v != "ico_1a9d4" {
		t.Fatalf("tag removed icon not reset: %s", v)
	}
	if v := read("SELECT appearance_value FROM tag_appearances WHERE id = 2"); v != "#00ff00" {
		t.Fatalf("tag colour row changed: %s", v)
	}
}
