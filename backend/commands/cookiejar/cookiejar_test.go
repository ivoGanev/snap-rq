package cookiejar

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"snap-rq/backend/commands"
	"snap-rq/backend/cookies"
	"snap-rq/backend/models"

	_ "modernc.org/sqlite"
)

func newTx(t *testing.T) *sql.Tx {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE projects (id INTEGER PRIMARY KEY); INSERT INTO projects (id) VALUES (1), (2);` + cookies.Schema); err != nil {
		t.Fatalf("creating schema: %v", err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	return tx
}

func raw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// run executes an undoable command the way the runner does: snapshot, run.
func run(t *testing.T, c commands.UndoableCommand, input any) (snapshot, result json.RawMessage, err error) {
	t.Helper()
	in := raw(t, input)
	snap, err := c.Snapshot(in)
	if err != nil {
		return nil, nil, err
	}
	out, err := c.Run(in)
	if err != nil {
		return nil, nil, err
	}
	return raw(t, snap), raw(t, out), nil
}

func listNames(t *testing.T, tx *sql.Tx, project int64) string {
	t.Helper()
	out, err := List{tx}.Run(raw(t, map[string]any{"project_id": project}))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range out.([]models.Cookie) {
		names = append(names, c.Domain+":"+c.Name+"="+c.Value)
	}
	return strings.Join(names, ",")
}

func TestSaveCreateEditAndUndo(t *testing.T) {
	tx := newTx(t)
	save := Save{tx}

	snap, created, err := run(t, save, models.Cookie{ProjectID: 1, Domain: ".Example.com", Name: "s", Value: "1"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	var c models.Cookie
	_ = json.Unmarshal(created, &c)
	if c.ID == 0 || c.Domain != "example.com" || c.Path != "/" {
		t.Fatalf("created = %+v, want id, lower-case domain without dot, path /", c)
	}

	c.Value = "2"
	editSnap, _, err := run(t, save, c)
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	if got := listNames(t, tx, 1); got != "example.com:s=2" {
		t.Errorf("after edit %q", got)
	}
	if err := save.Undo(editSnap, nil); err != nil {
		t.Fatalf("undo edit: %v", err)
	}
	if got := listNames(t, tx, 1); got != "example.com:s=1" {
		t.Errorf("after undoing the edit %q", got)
	}

	if err := save.Undo(snap, created); err != nil {
		t.Fatalf("undo create: %v", err)
	}
	if got := listNames(t, tx, 1); got != "" {
		t.Errorf("after undoing the create %q", got)
	}
}

func TestSaveRejectsBadInputAndDuplicates(t *testing.T) {
	tx := newTx(t)
	save := Save{tx}
	good := models.Cookie{ProjectID: 1, Domain: "example.com", Name: "s", Value: "1"}
	if _, _, err := run(t, save, good); err != nil {
		t.Fatal(err)
	}
	if _, _, err := run(t, save, good); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("duplicate: err = %v, want 'already exists'", err)
	}
	for name, bad := range map[string]models.Cookie{
		"no domain":     {ProjectID: 1, Name: "s"},
		"no name":       {ProjectID: 1, Domain: "example.com"},
		"relative path": {ProjectID: 1, Domain: "example.com", Name: "s", Path: "x"},
		"no project":    {Domain: "example.com", Name: "s"},
		"bad samesite":  {ProjectID: 1, Domain: "example.com", Name: "s", SameSite: "Maybe"},
	} {
		if _, _, err := run(t, save, bad); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestDeleteAndUndoKeepsId(t *testing.T) {
	tx := newTx(t)
	_, created, _ := run(t, Save{tx}, models.Cookie{ProjectID: 1, Domain: "example.com", Name: "s", Value: "1"})
	var c models.Cookie
	_ = json.Unmarshal(created, &c)

	del := Delete{tx}
	snap, _, err := run(t, del, map[string]any{"id": c.ID})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got := listNames(t, tx, 1); got != "" {
		t.Errorf("after delete %q", got)
	}
	if err := del.Undo(snap, nil); err != nil {
		t.Fatalf("undo: %v", err)
	}
	restored, err := loadCookie(tx, c.ID)
	if err != nil || restored.Value != "1" {
		t.Errorf("restored = %+v, %v; want the original row under the same id", restored, err)
	}
}

func TestClearByDomainAndAllWithUndo(t *testing.T) {
	tx := newTx(t)
	for _, c := range []models.Cookie{
		{ProjectID: 1, Domain: "a.com", Name: "x", Value: "1"},
		{ProjectID: 1, Domain: "a.com", Name: "y", Value: "1"},
		{ProjectID: 1, Domain: "b.com", Name: "x", Value: "1"},
		{ProjectID: 2, Domain: "a.com", Name: "x", Value: "other project"},
	} {
		if _, _, err := run(t, Save{tx}, c); err != nil {
			t.Fatal(err)
		}
	}

	clear := Clear{tx}
	snap, _, err := run(t, clear, map[string]any{"project_id": 1, "domain": "a.com"})
	if err != nil {
		t.Fatal(err)
	}
	if got := listNames(t, tx, 1); got != "b.com:x=1" {
		t.Errorf("after clearing a.com: %q", got)
	}
	if err := clear.Undo(snap, nil); err != nil {
		t.Fatal(err)
	}
	if got := listNames(t, tx, 1); got != "a.com:x=1,a.com:y=1,b.com:x=1" {
		t.Errorf("after undo: %q", got)
	}

	if _, _, err := run(t, clear, map[string]any{"project_id": 1}); err != nil {
		t.Fatal(err)
	}
	if got := listNames(t, tx, 1); got != "" {
		t.Errorf("after clearing all: %q", got)
	}
	if got := listNames(t, tx, 2); got != "a.com:x=other project" {
		t.Errorf("project 2 was touched: %q", got)
	}
}
