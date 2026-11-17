package mcp

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"snap-rq/backend/commands"
	"snap-rq/backend/commands/catalog"
	"snap-rq/backend/commands/requests"
	"snap-rq/backend/database"
)

// These tests run the tools against the real command stack on a real
// (temporary) database, so a tool whose input drifts from its command fails
// here, not in front of a user.

type harness struct {
	t      *testing.T
	s      *Server
	runner *commands.Runner
}

func newHarness(t *testing.T) harness {
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
	if err := runner.RegisterObservable(requests.NewSend); err != nil {
		t.Fatalf("registering observable commands: %v", err)
	}
	return harness{t: t, s: NewServer(runner, NewStore(db), "test"), runner: runner}
}

// run runs a command through the server's runner and fails the test on error.
func (h harness) run(command string, in any) any {
	h.t.Helper()
	out, err := h.s.run(command, in)
	if err != nil {
		h.t.Fatalf("%s: %v", command, err)
	}
	return out
}

// call runs a tool function and fails the test on error.
func (h harness) call(fn func() (any, error)) any {
	h.t.Helper()
	out, err := fn()
	if err != nil {
		h.t.Fatal(err)
	}
	return out
}

func (h harness) text(v any) string {
	h.t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		h.t.Fatal(err)
	}
	return string(raw)
}

// id reads the "id" of a command result.
func (h harness) id(v any) int64 {
	h.t.Helper()
	var row struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(h.text(v)), &row); err != nil || row.ID == 0 {
		h.t.Fatalf("no id in %s (%v)", h.text(v), err)
	}
	return row.ID
}

// firstID reads the "id" of the first row of a list result.
func (h harness) firstID(v any) int64 {
	h.t.Helper()
	var rows []struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(h.text(v)), &rows); err != nil || len(rows) == 0 {
		h.t.Fatalf("no rows in %s (%v)", h.text(v), err)
	}
	return rows[0].ID
}

// field reads one field of a stored request.
func (h harness) field(requestID int64, name string) string {
	h.t.Helper()
	var row map[string]any
	if err := json.Unmarshal([]byte(h.text(h.run("request.get", idInput{ID: requestID}))), &row); err != nil {
		h.t.Fatal(err)
	}
	return h.text(row[name])
}

func TestCatalogMatchesTheRealCommands(t *testing.T) {
	h := newHarness(t)
	var names []string
	for _, c := range h.runner.ListCommands() {
		names = append(names, c.Name)
	}
	if err := ValidateCatalog(names); err != nil {
		t.Fatal(err)
	}
}

func TestOrganizeFlowEndToEnd(t *testing.T) {
	h := newHarness(t)

	project := h.firstID(h.run("project.list", noInput{}))
	a := h.id(h.run("collection.create", createCollectionInput{ProjectID: project, Name: "A"}))
	b := h.id(h.run("collection.create", createCollectionInput{ProjectID: project, Name: "B"}))
	req := h.id(h.call(func() (any, error) {
		return createRequest(h.s, createRequestInput{CollectionID: a, Name: "r1", URL: "http://x"})
	}))

	// a move keeps every other field
	h.call(func() (any, error) { return moveRequests(h.s, moveRequestsInput{IDs: []int64{req}, CollectionID: b}) })
	if got, want := h.field(req, "collection_id"), h.text(b); got != want {
		t.Fatalf("collection_id = %s, want %s", got, want)
	}
	if got := h.field(req, "name"); got != `"r1"` {
		t.Fatalf("a move must keep the name, got %s", got)
	}
	if got := h.field(req, "url"); got != `"http://x"` {
		t.Fatalf("a move must keep the url, got %s", got)
	}

	// a bulk edit is undoable
	h.run("request.bulk_set", bulkEditInput{IDs: []int64{req}, Field: "name", Value: "renamed"})
	if got := h.field(req, "name"); got != `"renamed"` {
		t.Fatalf("name = %s", got)
	}
	if _, err := h.runner.Undo(); err != nil {
		t.Fatal(err)
	}
	if got := h.field(req, "name"); got != `"r1"` {
		t.Fatalf("undo should restore the name, got %s", got)
	}

	// tags
	h.run("tag.add", addTagInput{RequestID: req, ProjectID: project, TagName: "Auth"})
	if tags := h.text(h.run("tag.list", projectInput{ProjectID: project})); !strings.Contains(tags, "auth") {
		t.Fatalf("tag missing from %s", tags)
	}
	h.run("tag.rename", renameTagInput{ProjectID: project, OldName: "auth", NewName: "security"})
	h.run("tag.remove", removeTagInput{RequestID: req, TagName: "security"})

	// favourites
	fav := h.id(h.run("favourite.create", createFavouriteInput{ProjectID: project, Name: "Daily"}))
	h.run("favourite.add_request", addToFavouriteInput{FavouriteCollectionID: fav, RequestID: req})
	h.run("favourite.remove_requests", removeFromFavouriteInput{FavouriteCollectionID: fav, RequestIDs: []int64{req}})

	// trash round trip
	h.run("request.delete", deleteRequestsInput{IDs: []int64{req}})
	bin := h.firstID(h.run("bin.list", projectInput{ProjectID: project}))
	h.run("bin.restore", restoreInput{Items: []restoreItem{{BinID: bin, CollectionID: a}}})
	if got, want := h.field(req, "collection_id"), h.text(a); got != want {
		t.Fatalf("restored into %s, want %s", got, want)
	}
}
