package scripts_test

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"snap-rq/backend/commands"
	"snap-rq/backend/commands/catalog"
	"snap-rq/backend/database"
	"snap-rq/backend/models"
)

type fixture struct {
	t      *testing.T
	db     *sql.DB
	runner *commands.Runner
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	db, err := database.OpenPath(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("opening database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	runner := commands.NewRunner(db)
	if err := runner.Register(catalog.All...); err != nil {
		t.Fatalf("registering commands: %v", err)
	}
	return fixture{t: t, db: db, runner: runner}
}

func (f fixture) run(command string, in any) any {
	f.t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		f.t.Fatal(err)
	}
	out, err := f.runner.Run(command, raw)
	if err != nil {
		f.t.Fatalf("%s: %v", command, err)
	}
	return out
}

func (f fixture) undo() {
	f.t.Helper()
	if _, err := f.runner.Undo(); err != nil {
		f.t.Fatalf("undo: %v", err)
	}
}

func (f fixture) count(query string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.db.QueryRow(query, args...).Scan(&n); err != nil {
		f.t.Fatalf("%s: %v", query, err)
	}
	return n
}

// defaultProject returns the id of the default project and a new request in it.
func (f fixture) defaultProject() (projectID, requestID int64) {
	f.t.Helper()
	var collectionID int64
	if err := f.db.QueryRow(`SELECT id, project_id FROM collections LIMIT 1`).Scan(&collectionID, &projectID); err != nil {
		f.t.Fatalf("loading default collection: %v", err)
	}
	return projectID, f.newRequest(collectionID)
}

func (f fixture) newRequest(collectionID int64) int64 {
	f.t.Helper()
	res, err := f.db.Exec(
		`INSERT INTO http_requests (collection_id, name, url, method) VALUES (?, 'r', 'http://x', 'GET')`, collectionID)
	if err != nil {
		f.t.Fatalf("inserting request: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// otherProject creates a second project with one request.
func (f fixture) otherProject() (projectID, requestID int64) {
	f.t.Helper()
	res, err := f.db.Exec(`INSERT INTO projects (profile_id, name) SELECT profile_id, 'other' FROM projects LIMIT 1`)
	if err != nil {
		f.t.Fatalf("inserting project: %v", err)
	}
	projectID, _ = res.LastInsertId()
	res, err = f.db.Exec(`INSERT INTO collections (project_id, name) VALUES (?, 'other')`, projectID)
	if err != nil {
		f.t.Fatalf("inserting collection: %v", err)
	}
	collectionID, _ := res.LastInsertId()
	return projectID, f.newRequest(collectionID)
}

func TestAttachCreatesScriptWithAppearanceAndUndoRemovesOnlyItsLink(t *testing.T) {
	f := newFixture(t)
	project, first := f.defaultProject()
	second := f.newRequest(1)

	f.run("script.attach", map[string]any{"request_id": first, "project_id": project, "script_name": " Auth Token "})
	if n := f.count(`SELECT COUNT(*) FROM scripts WHERE name = 'Auth Token'`); n != 1 {
		t.Fatalf("scripts named 'Auth Token' = %d, want 1 (trimmed, case kept)", n)
	}
	if n := f.count(`SELECT COUNT(*) FROM script_appearances`); n != 1 {
		t.Errorf("appearances = %d, want the default one", n)
	}

	f.run("script.attach", map[string]any{"request_id": second, "project_id": project, "script_name": "Auth Token"})
	f.undo()

	// undoing the second attach must keep the first request's link
	if n := f.count(`SELECT COUNT(*) FROM request_scripts WHERE request_id = ?`, first); n != 1 {
		t.Errorf("first request links = %d after undoing the second attach, want 1", n)
	}
	if n := f.count(`SELECT COUNT(*) FROM request_scripts WHERE request_id = ?`, second); n != 0 {
		t.Errorf("second request links = %d after undo, want 0", n)
	}

	f.undo()
	if n := f.count(`SELECT COUNT(*) FROM scripts`); n != 0 {
		t.Errorf("scripts = %d after undoing the creating attach, want 0", n)
	}
}

func TestDetachOnlyTouchesTheRequestsOwnProject(t *testing.T) {
	f := newFixture(t)
	projectA, requestA := f.defaultProject()
	projectB, requestB := f.otherProject()
	f.run("script.attach", map[string]any{"request_id": requestA, "project_id": projectA, "script_name": "auth"})
	f.run("script.attach", map[string]any{"request_id": requestB, "project_id": projectB, "script_name": "auth"})

	f.run("script.detach", map[string]any{"request_id": requestB, "script_name": "auth"})

	if n := f.count(`SELECT COUNT(*) FROM request_scripts WHERE request_id = ?`, requestA); n != 1 {
		t.Errorf("project A link = %d, want 1", n)
	}
	if n := f.count(`SELECT COUNT(*) FROM request_scripts WHERE request_id = ?`, requestB); n != 0 {
		t.Errorf("project B link = %d, want 0", n)
	}

	f.undo()
	if n := f.count(`SELECT COUNT(*) FROM request_scripts WHERE request_id = ?`, requestB); n != 1 {
		t.Errorf("project B link = %d after undo, want 1", n)
	}
}

func TestRenameKeepsIdAndContentAndRefusesTakenNames(t *testing.T) {
	f := newFixture(t)
	project, request := f.defaultProject()
	created := f.run("script.attach", map[string]any{"request_id": request, "project_id": project, "script_name": "auth"}).(models.Script)
	f.run("script.attach", map[string]any{"request_id": 0, "project_id": project, "script_name": "logging"})
	f.run("script.set_content", map[string]any{"id": created.ID, "content": "sign(request)"})

	f.run("script.rename", map[string]any{"project_id": project, "old_name": "auth", "new_name": "Auth v2"})
	got := f.run("script.get", map[string]any{"id": created.ID}).(models.Script)
	if got.Name != "Auth v2" || got.Content != "sign(request)" {
		t.Errorf("after rename got %q / %q, want the same script renamed with its content", got.Name, got.Content)
	}
	if n := f.count(`SELECT COUNT(*) FROM request_scripts WHERE script_id = ?`, created.ID); n != 1 {
		t.Errorf("links after rename = %d, want 1", n)
	}

	raw, _ := json.Marshal(map[string]any{"project_id": project, "old_name": "Auth v2", "new_name": "logging"})
	if _, err := f.runner.Run("script.rename", raw); err == nil {
		t.Errorf("renaming onto an existing script name should fail")
	}

	f.undo()
	got = f.run("script.get", map[string]any{"id": created.ID}).(models.Script)
	if got.Name != "auth" {
		t.Errorf("name after undo = %q, want auth", got.Name)
	}
}

func TestDeleteUndoRestoresContentLinksAndAppearance(t *testing.T) {
	f := newFixture(t)
	project, request := f.defaultProject()
	created := f.run("script.attach", map[string]any{"request_id": request, "project_id": project, "script_name": "auth"}).(models.Script)
	f.run("script.set_content", map[string]any{"id": created.ID, "content": "sign(request)"})

	f.run("script.delete", map[string]any{"project_id": project, "script_name": "auth"})
	if n := f.count(`SELECT COUNT(*) FROM scripts`); n != 0 {
		t.Fatalf("scripts after delete = %d, want 0", n)
	}

	f.undo()
	got := f.run("script.get", map[string]any{"id": created.ID}).(models.Script)
	if got.Content != "sign(request)" {
		t.Errorf("content after undo = %q, want it restored", got.Content)
	}
	if n := f.count(`SELECT COUNT(*) FROM request_scripts WHERE script_id = ?`, created.ID); n != 1 {
		t.Errorf("links after undo = %d, want 1", n)
	}
	if n := f.count(`SELECT COUNT(*) FROM script_appearances WHERE script_id = ?`, created.ID); n != 1 {
		t.Errorf("appearances after undo = %d, want 1", n)
	}
}

func TestSetContentIsNotInUndoHistory(t *testing.T) {
	f := newFixture(t)
	project, _ := f.defaultProject()
	created := f.run("script.attach", map[string]any{"request_id": 0, "project_id": project, "script_name": "auth"}).(models.Script)
	before := len(f.runner.Steps())

	f.run("script.set_content", map[string]any{"id": created.ID, "content": "one"})
	f.run("script.set_content", map[string]any{"id": created.ID, "content": "two"})

	if after := len(f.runner.Steps()); after != before {
		t.Errorf("undo steps went from %d to %d; editing text must not add any", before, after)
	}
}
