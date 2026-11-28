package groups_test

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

// fixture runs the group commands the way the app does: through the runner,
// on a real database, so undo goes through the stored snapshots.
type fixture struct {
	t            *testing.T
	db           *sql.DB
	runner       *commands.Runner
	projectID    int64
	collectionID int64
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db, err := database.OpenPath(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	runner := commands.NewRunner(db)
	if err := runner.Register(catalog.All...); err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, db: db, runner: runner}
	if err := db.QueryRow(`SELECT id, project_id FROM collections ORDER BY id LIMIT 1`).
		Scan(&f.collectionID, &f.projectID); err != nil {
		t.Fatal(err)
	}
	return f
}

// run executes a command and decodes its result into out (when not nil).
func (f *fixture) run(name string, in, out any) {
	f.t.Helper()
	if err := f.try(name, in, out); err != nil {
		f.t.Fatalf("%s: %v", name, err)
	}
}

func (f *fixture) try(name string, in, out any) error {
	raw, err := json.Marshal(in)
	if err != nil {
		return err
	}
	result, err := f.runner.Run(name, raw)
	if err != nil {
		return err
	}
	if out != nil {
		encoded, _ := json.Marshal(result)
		return json.Unmarshal(encoded, out)
	}
	return nil
}

func (f *fixture) undo() {
	f.t.Helper()
	if _, err := f.runner.Undo(); err != nil {
		f.t.Fatalf("undo: %v", err)
	}
}

// requests adds requests to the collection with orders 0, 1, 2… and returns their ids.
func (f *fixture) requests(collectionID int64, names ...string) []int64 {
	f.t.Helper()
	ids := make([]int64, len(names))
	for i, name := range names {
		res, err := f.db.Exec(`INSERT INTO http_requests (collection_id, name, url, method, body_type, user_sort_order)
			VALUES (?, ?, '', 'GET', 'json', ?)`, collectionID, name, i)
		if err != nil {
			f.t.Fatal(err)
		}
		ids[i], _ = res.LastInsertId()
	}
	return ids
}

func (f *fixture) otherCollection() int64 {
	f.t.Helper()
	var c models.Collection
	f.run("collection.create", map[string]any{"project_id": f.projectID, "name": "other"}, &c)
	return c.ID
}

func (f *fixture) group(name string, ids ...int64) models.RequestGroup {
	f.t.Helper()
	var g models.RequestGroup
	f.run("group.create", map[string]any{"collection_id": f.collectionID, "name": name, "request_ids": ids}, &g)
	return g
}

// groupOf is the request's group id, 0 for none.
func (f *fixture) groupOf(requestID int64) int64 {
	f.t.Helper()
	var g sql.NullInt64
	if err := f.db.QueryRow(`SELECT group_id FROM http_requests WHERE id = ?`, requestID).Scan(&g); err != nil {
		f.t.Fatal(err)
	}
	return g.Int64
}

func (f *fixture) orderOf(requestID int64) int {
	f.t.Helper()
	var o int
	if err := f.db.QueryRow(`SELECT user_sort_order FROM http_requests WHERE id = ?`, requestID).Scan(&o); err != nil {
		f.t.Fatal(err)
	}
	return o
}

func (f *fixture) groupExists(id int64) bool {
	f.t.Helper()
	var n int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM request_groups WHERE id = ?`, id).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n == 1
}

func (f *fixture) storedGroup(id int64) models.RequestGroup {
	f.t.Helper()
	var g models.RequestGroup
	if err := f.db.QueryRow(`SELECT id, collection_id, name, color, sort_order FROM request_groups WHERE id = ?`, id).
		Scan(&g.ID, &g.CollectionID, &g.Name, &g.Color, &g.SortOrder); err != nil {
		f.t.Fatal(err)
	}
	return g
}

func TestCreateTakesThePlaceOfItsFirstRequest(t *testing.T) {
	f := newFixture(t)
	ids := f.requests(f.collectionID, "a", "b", "c")

	g := f.group("Auth", ids[1], ids[2])

	if g.SortOrder != 1 {
		t.Errorf("group sort_order = %d, want 1 (the first member's place)", g.SortOrder)
	}
	for _, id := range ids[1:] {
		if f.groupOf(id) != g.ID {
			t.Errorf("request %d is in group %d, want %d", id, f.groupOf(id), g.ID)
		}
	}
	if f.groupOf(ids[0]) != 0 {
		t.Errorf("a request not picked joined the group")
	}
}

func TestCreateNeedsARequest(t *testing.T) {
	f := newFixture(t)
	err := f.try("group.create", map[string]any{"collection_id": f.collectionID, "name": "Empty", "request_ids": []int64{}}, nil)
	if err == nil {
		t.Fatal("creating a group with no requests succeeded")
	}
}

func TestCreateRemovesTheGroupItEmptiesAndUndoBringsItBack(t *testing.T) {
	f := newFixture(t)
	ids := f.requests(f.collectionID, "a", "b")
	old := f.group("Old", ids[0])

	newer := f.group("New", ids[0])
	if f.groupExists(old.ID) {
		t.Fatal("the group left empty was kept")
	}

	f.undo()
	if f.groupExists(newer.ID) {
		t.Error("undo kept the new group")
	}
	if !f.groupExists(old.ID) {
		t.Fatal("undo did not bring the emptied group back")
	}
	if f.groupOf(ids[0]) != old.ID {
		t.Errorf("after undo the request is in group %d, want %d", f.groupOf(ids[0]), old.ID)
	}
}

func TestSetRequestsJoinsOnlyGroupsOfTheSameCollection(t *testing.T) {
	f := newFixture(t)
	ids := f.requests(f.collectionID, "a", "b")
	g := f.group("Auth", ids[0])
	foreign := f.requests(f.otherCollection(), "x")[0]

	f.run("group.set_requests", map[string]any{"group_id": g.ID, "request_ids": []int64{ids[1], foreign}}, nil)

	if f.groupOf(ids[1]) != g.ID {
		t.Errorf("a request of the group's collection did not join it")
	}
	if f.groupOf(foreign) != 0 {
		t.Errorf("a request of another collection joined the group")
	}
}

func TestSetRequestsRemovesTheEmptiedGroupAndUndoRestoresIt(t *testing.T) {
	f := newFixture(t)
	ids := f.requests(f.collectionID, "a", "b")
	g := f.group("Auth", ids...)

	f.run("group.set_requests", map[string]any{"group_id": 0, "request_ids": ids}, nil)
	if f.groupExists(g.ID) {
		t.Fatal("a group with no requests left was kept")
	}

	f.undo()
	if !f.groupExists(g.ID) {
		t.Fatal("undo did not bring the group back")
	}
	for _, id := range ids {
		if f.groupOf(id) != g.ID {
			t.Errorf("after undo request %d is in group %d, want %d", id, f.groupOf(id), g.ID)
		}
	}
}

func TestDeleteKeepsTheRequestsAndUndoRestoresTheSameGroup(t *testing.T) {
	f := newFixture(t)
	ids := f.requests(f.collectionID, "a", "b")
	g := f.group("Auth", ids...)
	slot := "secondary"
	f.run("group.update", models.RequestGroup{ID: g.ID, CollectionID: g.CollectionID, Name: "Auth", Color: &slot}, nil)

	f.run("group.delete", map[string]any{"id": g.ID}, nil)
	if f.groupExists(g.ID) {
		t.Fatal("the group was not deleted")
	}
	for _, id := range ids {
		if f.groupOf(id) != 0 {
			t.Errorf("request %d still points at the deleted group", id)
		}
	}

	f.undo()
	restored := f.storedGroup(g.ID)
	if restored.Name != "Auth" || restored.Color == nil || *restored.Color != slot {
		t.Errorf("undo restored %+v, want the same name and colour", restored)
	}
	for _, id := range ids {
		if f.groupOf(id) != g.ID {
			t.Errorf("after undo request %d is in group %d, want %d", id, f.groupOf(id), g.ID)
		}
	}
}

func TestUpdateRenamesAndColoursAndUndoRestores(t *testing.T) {
	f := newFixture(t)
	g := f.group("Auth", f.requests(f.collectionID, "a")...)

	bad := "not-a-slot"
	if err := f.try("group.update", models.RequestGroup{ID: g.ID, Name: "Auth", Color: &bad}, nil); err == nil {
		t.Error("an unknown colour was accepted")
	}

	slot := "tertiary"
	f.run("group.update", models.RequestGroup{ID: g.ID, Name: "Login", Color: &slot}, nil)
	if got := f.storedGroup(g.ID); got.Name != "Login" || got.Color == nil || *got.Color != slot {
		t.Fatalf("after update the group is %+v", got)
	}

	f.undo()
	if got := f.storedGroup(g.ID); got.Name != "Auth" || got.Color != nil {
		t.Errorf("undo left %+v, want the old name and no colour", got)
	}
}

func TestListOrdersByPlaceThenName(t *testing.T) {
	f := newFixture(t)
	ids := f.requests(f.collectionID, "a", "b", "c")
	late := f.group("Zeta", ids[0])
	early := f.group("Beta", ids[1])
	f.run("group.arrange", map[string]any{"collection_id": f.collectionID, "items": []map[string]any{
		{"kind": "group", "id": early.ID},
		{"kind": "request", "id": ids[1], "group_id": early.ID},
		{"kind": "group", "id": late.ID},
		{"kind": "request", "id": ids[0], "group_id": late.ID},
		{"kind": "request", "id": ids[2]},
	}}, nil)

	var listed []models.RequestGroup
	f.run("group.list", map[string]any{"project_id": f.projectID}, &listed)
	var names []string
	for _, g := range listed {
		if g.CollectionID == f.collectionID {
			names = append(names, g.Name)
		}
	}
	if len(names) != 2 || names[0] != "Beta" || names[1] != "Zeta" {
		t.Errorf("listed %v, want [Beta Zeta] by their places", names)
	}
}

type arranged struct {
	RequestIDs      []int64 `json:"request_ids"`
	GroupIDs        []int64 `json:"group_ids"`
	RemovedGroupIDs []int64 `json:"removed_group_ids"`
}

func TestArrangeWritesAndReportsOnlyTheRowsThatMoved(t *testing.T) {
	f := newFixture(t)
	ids := f.requests(f.collectionID, "a", "b", "c")

	var out arranged
	f.run("group.arrange", map[string]any{"collection_id": f.collectionID, "items": []map[string]any{
		{"kind": "request", "id": ids[0]},
		{"kind": "request", "id": ids[2]},
		{"kind": "request", "id": ids[1]},
	}}, &out)

	moved := map[int64]bool{}
	for _, id := range out.RequestIDs {
		moved[id] = true
	}
	if len(moved) != 2 || !moved[ids[1]] || !moved[ids[2]] {
		t.Errorf("reported %v as moved, want only b and c", out.RequestIDs)
	}
	if f.orderOf(ids[2]) != 1 || f.orderOf(ids[1]) != 2 {
		t.Errorf("orders after the swap: c=%d b=%d, want 1 and 2", f.orderOf(ids[2]), f.orderOf(ids[1]))
	}
}

func TestArrangeIgnoresRequestsOfOtherCollections(t *testing.T) {
	f := newFixture(t)
	ids := f.requests(f.collectionID, "a")
	g := f.group("Auth", ids[0])
	foreign := f.requests(f.otherCollection(), "x")[0]

	f.run("group.arrange", map[string]any{"collection_id": f.collectionID, "items": []map[string]any{
		{"kind": "group", "id": g.ID},
		{"kind": "request", "id": ids[0], "group_id": g.ID},
		{"kind": "request", "id": foreign, "group_id": g.ID},
	}}, nil)

	if f.groupOf(foreign) != 0 {
		t.Error("a request of another collection was put in the group")
	}
}

func TestArrangeRemovesEmptiedGroupsAndUndoRestoresEverything(t *testing.T) {
	f := newFixture(t)
	ids := f.requests(f.collectionID, "a", "b", "c")
	g := f.group("Auth", ids[1])
	before := map[int64]int{}
	for _, id := range ids {
		before[id] = f.orderOf(id)
	}

	var out arranged
	f.run("group.arrange", map[string]any{"collection_id": f.collectionID, "items": []map[string]any{
		{"kind": "request", "id": ids[1]},
		{"kind": "request", "id": ids[0]},
		{"kind": "group", "id": g.ID},
		{"kind": "request", "id": ids[2]},
	}}, &out)
	if f.groupExists(g.ID) {
		t.Fatal("the group dragged empty was kept")
	}
	if len(out.RemovedGroupIDs) != 1 || out.RemovedGroupIDs[0] != g.ID {
		t.Errorf("reported removed groups %v, want [%d]", out.RemovedGroupIDs, g.ID)
	}

	f.undo()
	if !f.groupExists(g.ID) {
		t.Fatal("undo did not bring the group back")
	}
	if f.groupOf(ids[1]) != g.ID {
		t.Errorf("after undo b is in group %d, want %d", f.groupOf(ids[1]), g.ID)
	}
	for _, id := range ids {
		if f.orderOf(id) != before[id] {
			t.Errorf("after undo request %d has order %d, want %d", id, f.orderOf(id), before[id])
		}
	}
}

func TestArrangeWritesCorrectlyAcrossItsBatches(t *testing.T) {
	f := newFixture(t)
	names := make([]string, 1203)
	for i := range names {
		names[i] = "r"
	}
	ids := f.requests(f.collectionID, names...)

	// Reversed: every row moves, more than two batches' worth.
	items := make([]map[string]any, len(ids))
	for i := range ids {
		items[i] = map[string]any{"kind": "request", "id": ids[len(ids)-1-i]}
	}
	f.run("group.arrange", map[string]any{"collection_id": f.collectionID, "items": items}, nil)
	for i, id := range ids {
		if got, want := f.orderOf(id), len(ids)-1-i; got != want {
			t.Fatalf("request %d of %d has order %d, want %d", i, len(ids), got, want)
		}
	}

	f.undo()
	for i, id := range ids {
		if got := f.orderOf(id); got != i {
			t.Fatalf("after undo request %d has order %d, want %d", i, got, i)
		}
	}
}

func TestTwoQuickGroupActionsStaySeparateUndoSteps(t *testing.T) {
	f := newFixture(t)
	ids := f.requests(f.collectionID, "a", "b")
	g := f.group("Auth", ids[0])

	// Right after the create, well inside the window where repeats merge.
	f.run("group.set_requests", map[string]any{"group_id": g.ID, "request_ids": []int64{ids[1]}}, nil)

	f.undo()
	if !f.groupExists(g.ID) || f.groupOf(ids[0]) != g.ID {
		t.Fatal("undoing the second action also undid the first")
	}
	if f.groupOf(ids[1]) != 0 {
		t.Error("undo left b in the group")
	}
}

func (f *fixture) moveToCollection(requestID, collectionID int64) {
	f.t.Helper()
	var req models.HttpRequest
	f.run("request.get", map[string]any{"id": requestID}, &req)
	req.CollectionID = collectionID
	f.run("request.update", req, nil)
}

func TestMovingARequestToAnotherCollectionTakesItOutOfItsGroup(t *testing.T) {
	f := newFixture(t)
	ids := f.requests(f.collectionID, "a", "b")
	g := f.group("Auth", ids...)

	f.moveToCollection(ids[0], f.otherCollection())

	if f.groupOf(ids[0]) != 0 {
		t.Error("a request moved to another collection kept its group")
	}
	if !f.groupExists(g.ID) {
		t.Error("the group was deleted while a request was still in it")
	}
}

func TestMovingAGroupsLastRequestAwayRemovesTheGroup(t *testing.T) {
	f := newFixture(t)
	ids := f.requests(f.collectionID, "a")
	g := f.group("Auth", ids[0])

	f.moveToCollection(ids[0], f.otherCollection())

	if f.groupExists(g.ID) {
		t.Error("the group left empty by the move was kept")
	}
}

func TestADuplicateStaysInTheOriginalsGroup(t *testing.T) {
	f := newFixture(t)
	ids := f.requests(f.collectionID, "a")
	g := f.group("Auth", ids[0])

	var copied models.HttpRequest
	f.run("request.duplicate", map[string]any{"id": ids[0]}, &copied)

	if f.groupOf(copied.ID) != g.ID {
		t.Errorf("the duplicate is in group %d, want %d", f.groupOf(copied.ID), g.ID)
	}
}

func TestANewRequestGoesAfterTheCollectionsGroups(t *testing.T) {
	f := newFixture(t)
	ids := f.requests(f.collectionID, "a", "b")
	g := f.group("Auth", ids[0])
	if _, err := f.db.Exec(`UPDATE request_groups SET sort_order = 50 WHERE id = ?`, g.ID); err != nil {
		t.Fatal(err)
	}

	var created models.HttpRequest
	f.run("request.create", map[string]any{"collection_id": f.collectionID, "name": "new", "method": "GET"}, &created)

	if f.orderOf(created.ID) <= 50 {
		t.Errorf("the new request's order is %d, want after the group's 50", f.orderOf(created.ID))
	}
}

func TestDeletingACollectionRemovesItsGroups(t *testing.T) {
	f := newFixture(t)
	other := f.otherCollection()
	ids := f.requests(other, "a")
	var g models.RequestGroup
	f.run("group.create", map[string]any{"collection_id": other, "name": "Auth", "request_ids": ids}, &g)

	f.run("collection.delete", map[string]any{"id": other}, nil)

	if f.groupExists(g.ID) {
		t.Error("the deleted collection's group is still there")
	}
}
