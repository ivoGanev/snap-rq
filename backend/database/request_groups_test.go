package database

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func openTestDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := OpenPath(path)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func defaultCollection(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow(`SELECT id FROM collections ORDER BY id LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestGroupsFromBeforeOrderingAreNumberedByName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	db := openTestDB(t, path)
	collection := defaultCollection(t, db)
	for _, name := range []string{"Zeta", "Alpha", "Mid"} {
		if _, err := db.Exec(`INSERT INTO request_groups (collection_id, name) VALUES (?, ?)`, collection, name); err != nil {
			t.Fatal(err)
		}
	}
	// As the table was before groups had an order of their own.
	if _, err := db.Exec(`ALTER TABLE request_groups DROP COLUMN sort_order`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	db = openTestDB(t, path)
	defer db.Close()
	rows, err := db.Query(`SELECT name FROM request_groups WHERE collection_id = ? ORDER BY sort_order`, collection)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	if len(names) != 3 || names[0] != "Alpha" || names[1] != "Mid" || names[2] != "Zeta" {
		t.Errorf("groups by sort_order after the upgrade: %v, want [Alpha Mid Zeta]", names)
	}
}

func TestRequestGroupIDIsIndexed(t *testing.T) {
	db := openTestDB(t, filepath.Join(t.TempDir(), "app.db"))
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'index' AND tbl_name = 'http_requests' AND sql LIKE '%(group_id)%'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Error("http_requests.group_id has no index; deleting a group would scan every request")
	}
}

func TestDeletingAGroupKeepsItsRequests(t *testing.T) {
	db := openTestDB(t, filepath.Join(t.TempDir(), "app.db"))
	defer db.Close()
	collection := defaultCollection(t, db)
	res, err := db.Exec(`INSERT INTO request_groups (collection_id, name) VALUES (?, 'Auth')`, collection)
	if err != nil {
		t.Fatal(err)
	}
	group, _ := res.LastInsertId()
	res, err = db.Exec(`INSERT INTO http_requests (collection_id, name, url, method, body_type, group_id)
		VALUES (?, 'r', '', 'GET', 'json', ?)`, collection, group)
	if err != nil {
		t.Fatal(err)
	}
	request, _ := res.LastInsertId()

	if _, err := db.Exec(`DELETE FROM request_groups WHERE id = ?`, group); err != nil {
		t.Fatal(err)
	}

	var groupID sql.NullInt64
	if err := db.QueryRow(`SELECT group_id FROM http_requests WHERE id = ?`, request).Scan(&groupID); err != nil {
		t.Fatalf("the request went with its group: %v", err)
	}
	if groupID.Valid {
		t.Errorf("the request still points at deleted group %d", groupID.Int64)
	}
}
