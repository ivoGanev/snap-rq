package cookiejar

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"snap-rq/backend/commands"
	"snap-rq/backend/cookies"
	"snap-rq/backend/models"
	"snap-rq/backend/secrets"

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
	secrets.SetDefault(secrets.NewVault(secrets.MemoryKeyStore()))
	if _, err := db.Exec(`CREATE TABLE projects (id INTEGER PRIMARY KEY); INSERT INTO projects (id) VALUES (1), (2);
		CREATE TABLE app_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);` + cookies.Schema); err != nil {
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

// save runs cookie.save (not undoable) and returns the saved cookie.
func save(t *testing.T, tx *sql.Tx, c models.Cookie) (models.Cookie, error) {
	t.Helper()
	out, err := Save{tx}.Run(raw(t, c))
	if err != nil {
		return models.Cookie{}, err
	}
	return out.(models.Cookie), nil
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

func TestSaveCreateAndEditStoreTheValueEncrypted(t *testing.T) {
	tx := newTx(t)

	c, err := save(t, tx, models.Cookie{ProjectID: 1, Domain: ".Example.com", Name: "s", Value: "1"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.ID == 0 || c.Domain != "example.com" || c.Path != "/" || c.Value != "1" {
		t.Fatalf("created = %+v, want id, lower-case domain without dot, path /, the plain value", c)
	}

	c.Value = "2"
	if _, err := save(t, tx, c); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if got := listNames(t, tx, 1); got != "example.com:s=2" {
		t.Errorf("after edit %q", got)
	}
	var stored string
	tx.QueryRow(`SELECT value FROM cookies WHERE id = ?`, c.ID).Scan(&stored)
	if !secrets.IsEncrypted(stored) {
		t.Errorf("stored value %q, want encrypted", stored)
	}
}

func TestSaveRejectsBadInputAndDuplicates(t *testing.T) {
	tx := newTx(t)
	good := models.Cookie{ProjectID: 1, Domain: "example.com", Name: "s", Value: "1"}
	if _, err := save(t, tx, good); err != nil {
		t.Fatal(err)
	}
	if _, err := save(t, tx, good); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("duplicate: err = %v, want 'already exists'", err)
	}
	for name, bad := range map[string]models.Cookie{
		"no domain":     {ProjectID: 1, Name: "s"},
		"no name":       {ProjectID: 1, Domain: "example.com"},
		"relative path": {ProjectID: 1, Domain: "example.com", Name: "s", Path: "x"},
		"no project":    {Domain: "example.com", Name: "s"},
		"bad samesite":  {ProjectID: 1, Domain: "example.com", Name: "s", SameSite: "Maybe"},
	} {
		if _, err := save(t, tx, bad); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestDeleteAndUndoKeepsId(t *testing.T) {
	tx := newTx(t)
	saved, _ := save(t, tx, models.Cookie{ProjectID: 1, Domain: "example.com", Name: "s", Value: "1"})
	created := raw(t, saved)
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
	if err != nil || !secrets.IsEncrypted(restored.Value) {
		t.Fatalf("restored = %+v, %v; want the original row under the same id, still encrypted", restored, err)
	}
	if strings.Contains(string(snap), `"1"`) {
		t.Errorf("the undo snapshot holds the plain value: %s", snap)
	}
	if plain, err := cookies.Open(tx, restored.Value); err != nil || plain != "1" {
		t.Errorf("restored value decrypts to %q, %v; want 1", plain, err)
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
		if _, err := save(t, tx, c); err != nil {
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
