package services

import (
	"testing"

	"snap-rq/backend/models"
)

func TestUserSortOrderAppendAndReorder(t *testing.T) {
	db := newTestDB(t)
	runMigrations(t, db)

	if _, err := db.Exec("INSERT INTO projects (id, project_id) VALUES (1, 1)"); err != nil {
		// projects table only has profile-less minimal columns in the test schema.
		t.Logf("inserting project: %v", err)
	}
	if _, err := db.Exec("INSERT INTO collections (id, project_id, name) VALUES (10, 1, 'c1'), (11, 1, 'c2')"); err != nil {
		t.Fatalf("inserting collections: %v", err)
	}

	rs := NewRequestService(db)

	// New requests are appended to the end of their collection's order.
	var ids []int64
	for _, name := range []string{"alpha", "bravo", "charlie"} {
		req, err := rs.CreateRequest(models.HttpRequest{CollectionID: 10, Name: name, URL: "http://x", Method: "GET"})
		if err != nil {
			t.Fatalf("creating request %s: %v", name, err)
		}
		ids = append(ids, req.ID)
		if req.UserSortOrder != int(len(ids)-1) {
			t.Fatalf("request %s got user_sort_order %d, want %d", name, req.UserSortOrder, len(ids)-1)
		}
	}

	// Reordering persists and reads back in the new order.
	if err := rs.UpdateCollectionRequestOrder(10, []int64{ids[2], ids[0], ids[1]}); err != nil {
		t.Fatalf("reordering collection: %v", err)
	}
	got, err := rs.GetRequestsForCollection(10)
	if err != nil {
		t.Fatalf("listing collection: %v", err)
	}
	wantNames := []string{"charlie", "alpha", "bravo"}
	for i, req := range got {
		if req.Name != wantNames[i] {
			t.Fatalf("collection order position %d = %s, want %s", i, req.Name, wantNames[i])
		}
		if req.UserSortOrder != i {
			t.Fatalf("request %s user_sort_order = %d, want %d", req.Name, req.UserSortOrder, i)
		}
	}

	// Moving a request to another collection appends it to the target order.
	moved, err := rs.UpdateRequest(models.HttpRequest{
		ID: ids[0], CollectionID: 11, Name: "alpha", URL: "http://x", Method: "GET",
	})
	if err != nil {
		t.Fatalf("moving request: %v", err)
	}
	if moved.UserSortOrder != 0 {
		t.Fatalf("moved request user_sort_order = %d, want 0", moved.UserSortOrder)
	}
}

func TestFavouriteUserSortOrder(t *testing.T) {
	db := newTestDB(t)
	runMigrations(t, db)

	if _, err := db.Exec("INSERT INTO collections (id, project_id, name) VALUES (10, 1, 'c1')"); err != nil {
		t.Fatalf("inserting collection: %v", err)
	}
	if _, err := db.Exec("INSERT INTO favourite_collections (id, project_id, name) VALUES (20, 1, 'fav')"); err != nil {
		t.Fatalf("inserting favourite collection: %v", err)
	}

	rs := NewRequestService(db)
	fs := NewFavouriteService(db)

	var ids []int64
	for _, name := range []string{"one", "two", "three"} {
		req, err := rs.CreateRequest(models.HttpRequest{CollectionID: 10, Name: name, URL: "http://x", Method: "GET"})
		if err != nil {
			t.Fatalf("creating request %s: %v", name, err)
		}
		ids = append(ids, req.ID)
		if _, err := fs.AddRequestToFavouriteCollection(20, req.ID); err != nil {
			t.Fatalf("adding favourite %s: %v", name, err)
		}
	}

	if err := fs.UpdateFavouriteRequestOrder(20, []int64{ids[1], ids[2], ids[0]}); err != nil {
		t.Fatalf("reordering favourites: %v", err)
	}
	got, err := fs.GetRequestsForFavouriteCollection(20)
	if err != nil {
		t.Fatalf("listing favourites: %v", err)
	}
	wantNames := []string{"two", "three", "one"}
	for i, req := range got {
		if req.Name != wantNames[i] {
			t.Fatalf("favourite order position %d = %s, want %s", i, req.Name, wantNames[i])
		}
	}
}

func TestTagUserSortOrder(t *testing.T) {
	db := newTestDB(t)
	runMigrations(t, db)

	if _, err := db.Exec("INSERT INTO collections (id, project_id, name) VALUES (10, 1, 'c1')"); err != nil {
		t.Fatalf("inserting collection: %v", err)
	}

	rs := NewRequestService(db)
	ts := NewTagService(db)

	var ids []int64
	for _, name := range []string{"one", "two", "three"} {
		req, err := rs.CreateRequest(models.HttpRequest{CollectionID: 10, Name: name, URL: "http://x", Method: "GET"})
		if err != nil {
			t.Fatalf("creating request %s: %v", name, err)
		}
		ids = append(ids, req.ID)
		if _, err := ts.AddTagToRequest(req.ID, 1, "urgent"); err != nil {
			t.Fatalf("tagging request %s: %v", name, err)
		}
	}

	if err := ts.UpdateTagRequestOrder(1, "urgent", []int64{ids[2], ids[0], ids[1]}); err != nil {
		t.Fatalf("reordering tag: %v", err)
	}
	got, err := ts.GetRequestsForTag(1, "urgent")
	if err != nil {
		t.Fatalf("listing tag requests: %v", err)
	}
	wantNames := []string{"three", "one", "two"}
	for i, req := range got {
		if req.Name != wantNames[i] {
			t.Fatalf("tag order position %d = %s, want %s", i, req.Name, wantNames[i])
		}
	}
}

func TestDuplicateRequestAppendsToOrders(t *testing.T) {
	db := newTestDB(t)
	runMigrations(t, db)

	if _, err := db.Exec("INSERT INTO collections (id, project_id, name) VALUES (10, 1, 'c1')"); err != nil {
		t.Fatalf("inserting collection: %v", err)
	}
	if _, err := db.Exec("INSERT INTO favourite_collections (id, project_id, name) VALUES (20, 1, 'fav')"); err != nil {
		t.Fatalf("inserting favourite collection: %v", err)
	}

	rs := NewRequestService(db)
	ts := NewTagService(db)
	fs := NewFavouriteService(db)

	original, err := rs.CreateRequest(models.HttpRequest{CollectionID: 10, Name: "orig", URL: "http://x", Method: "GET"})
	if err != nil {
		t.Fatalf("creating original: %v", err)
	}
	if _, err := ts.AddTagToRequest(original.ID, 1, "urgent"); err != nil {
		t.Fatalf("tagging original: %v", err)
	}
	if _, err := fs.AddRequestToFavouriteCollection(20, original.ID); err != nil {
		t.Fatalf("favouriting original: %v", err)
	}

	dup, err := rs.DuplicateRequest(original.ID)
	if err != nil {
		t.Fatalf("duplicating: %v", err)
	}
	if dup.UserSortOrder != 1 {
		t.Fatalf("duplicate collection order = %d, want 1", dup.UserSortOrder)
	}

	favReqs, err := fs.GetRequestsForFavouriteCollection(20)
	if err != nil {
		t.Fatalf("listing favourites: %v", err)
	}
	if len(favReqs) != 2 || favReqs[1].ID != dup.ID || favReqs[1].UserSortOrder <= favReqs[0].UserSortOrder {
		t.Fatalf("duplicate not appended to favourite order: %+v", favReqs)
	}

	tagReqs, err := ts.GetRequestsForTag(1, "urgent")
	if err != nil {
		t.Fatalf("listing tag: %v", err)
	}
	if len(tagReqs) != 2 || tagReqs[1].ID != dup.ID || tagReqs[1].UserSortOrder <= tagReqs[0].UserSortOrder {
		t.Fatalf("duplicate not appended to tag order: %+v", tagReqs)
	}
}

func strPtr(s string) *string { return &s }

func TestBulkRequestColor(t *testing.T) {
	db := newTestDB(t)
	runMigrations(t, db)

	if _, err := db.Exec("INSERT INTO collections (id, project_id, name) VALUES (10, 1, 'c1'), (11, 1, 'c2')"); err != nil {
		t.Fatalf("inserting collections: %v", err)
	}

	rs := NewRequestService(db)

	var ids []int64
	for _, name := range []string{"one", "two"} {
		req, err := rs.CreateRequest(models.HttpRequest{CollectionID: 10, Name: name, URL: "http://x", Method: "GET"})
		if err != nil {
			t.Fatalf("creating request %s: %v", name, err)
		}
		ids = append(ids, req.ID)
	}
	other, err := rs.CreateRequest(models.HttpRequest{CollectionID: 11, Name: "other", URL: "http://x", Method: "GET"})
	if err != nil {
		t.Fatalf("creating other request: %v", err)
	}

	blue := "#AECBFA"
	updated, err := rs.BulkUpdateRequestColor(ids, strPtr(blue))
	if err != nil {
		t.Fatalf("bulk colour update: %v", err)
	}
	for _, req := range updated {
		if req.UserColor == nil || *req.UserColor != blue {
			t.Fatalf("expected colour %q on request %d, got %v", blue, req.ID, req.UserColor)
		}
	}

	got, err := rs.GetRequestsForCollection(10)
	if err != nil {
		t.Fatalf("listing collection: %v", err)
	}
	for _, req := range got {
		if req.UserColor == nil || *req.UserColor != blue {
			t.Fatalf("expected persisted colour, got %v", req.UserColor)
		}
	}

	// Clearing via nil.
	if _, err := rs.BulkUpdateRequestColor(ids, nil); err != nil {
		t.Fatalf("clearing colour: %v", err)
	}
	got, _ = rs.GetRequestsForCollection(10)
	for _, req := range got {
		if req.UserColor != nil {
			t.Fatalf("expected cleared colour, got %v", req.UserColor)
		}
	}

	// The other collection's request is untouched.
	otherGot, err := rs.GetRequestsForCollection(11)
	if err != nil {
		t.Fatalf("listing other collection: %v", err)
	}
	if otherGot[0].UserColor != nil || otherGot[0].ID != other.ID {
		t.Fatalf("unexpected colour on unrelated request: %+v", otherGot[0])
	}
}

func TestBulkFavouriteAndTagColor(t *testing.T) {
	db := newTestDB(t)
	runMigrations(t, db)

	if _, err := db.Exec("INSERT INTO collections (id, project_id, name) VALUES (10, 1, 'c1')"); err != nil {
		t.Fatalf("inserting collection: %v", err)
	}
	if _, err := db.Exec("INSERT INTO favourite_collections (id, project_id, name) VALUES (20, 1, 'fav')"); err != nil {
		t.Fatalf("inserting favourite collection: %v", err)
	}

	rs := NewRequestService(db)
	fs := NewFavouriteService(db)
	ts := NewTagService(db)

	var ids []int64
	for _, name := range []string{"one", "two"} {
		req, err := rs.CreateRequest(models.HttpRequest{CollectionID: 10, Name: name, URL: "http://x", Method: "GET"})
		if err != nil {
			t.Fatalf("creating request %s: %v", name, err)
		}
		ids = append(ids, req.ID)
		if _, err := fs.AddRequestToFavouriteCollection(20, req.ID); err != nil {
			t.Fatalf("favouriting %s: %v", name, err)
		}
		if _, err := ts.AddTagToRequest(req.ID, 1, "urgent"); err != nil {
			t.Fatalf("tagging %s: %v", name, err)
		}
	}

	green := "#B6E2C6"
	if err := fs.BulkUpdateFavouriteRequestColor(20, ids, strPtr(green)); err != nil {
		t.Fatalf("bulk favourite colour: %v", err)
	}
	favReqs, err := fs.GetRequestsForFavouriteCollection(20)
	if err != nil {
		t.Fatalf("listing favourites: %v", err)
	}
	for _, req := range favReqs {
		if req.UserColor == nil || *req.UserColor != green {
			t.Fatalf("expected favourite colour %q, got %v", green, req.UserColor)
		}
	}

	yellow := "#F7E6A3"
	if err := ts.BulkUpdateTagRequestColor(1, "urgent", ids, strPtr(yellow)); err != nil {
		t.Fatalf("bulk tag colour: %v", err)
	}
	tagReqs, err := ts.GetRequestsForTag(1, "urgent")
	if err != nil {
		t.Fatalf("listing tag requests: %v", err)
	}
	for _, req := range tagReqs {
		if req.UserColor == nil || *req.UserColor != yellow {
			t.Fatalf("expected tag colour %q, got %v", yellow, req.UserColor)
		}
	}
}
