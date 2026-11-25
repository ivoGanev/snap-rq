package requests_test

import (
	"testing"

	"snap-rq/backend/models"
)

// deletable is a request with everything that can hang off it: two
// responses (one with a script log), an enabled and a disabled script, and a
// coloured favourite.
func deletable(h *harness) int64 {
	h.t.Helper()
	id := h.create(models.HttpRequest{
		Name: "Keep me", Method: "POST", URL: h.url("/x"), BodyType: "json", Body: `{"a":1}`,
		RequestHeaders: "X-Kept: yes",
	})
	h.attach(id, "logs", `function afterResponse(res, ctx) { console.log("logged"); }`)
	h.attach(id, "off", `function afterResponse(res, ctx) {}`)
	if _, err := h.db.Exec(`UPDATE request_scripts SET enabled = 0
		WHERE script_id = (SELECT id FROM scripts WHERE name = 'off')`); err != nil {
		h.t.Fatal(err)
	}
	h.mustSend(id, sendOptions{})
	h.mustSend(id, sendOptions{})

	var favourite struct {
		ID int64 `json:"id"`
	}
	h.run("favourite.create", map[string]any{"project_id": h.projectID, "name": "Daily"}, &favourite)
	h.run("favourite.add_request", map[string]any{"favourite_collection_id": favourite.ID, "request_id": id}, nil)
	h.run("favourite.set_color", map[string]any{
		"favourite_collection_id": favourite.ID, "request_ids": []int64{id}, "color": "secondary",
	}, nil)
	return id
}

// checkComplete verifies a request deletable made came back whole.
func checkComplete(h *harness, id int64) {
	h.t.Helper()
	var req models.HttpRequest
	h.run("request.get", map[string]any{"id": id}, &req)
	if req.Name != "Keep me" || req.Body != `{"a":1}` || req.RequestHeaders != "X-Kept: yes" || req.StatusCode != 200 {
		h.t.Errorf("request = %+v", req)
	}

	history := h.history(id)
	if len(history) != 2 {
		h.t.Fatalf("history = %d responses, want 2", len(history))
	}
	for _, res := range history {
		if res.Method != "POST" || res.URL != h.url("/x") {
			h.t.Errorf("response %d was sent as %q %q, want POST %s", res.ID, res.Method, res.URL, h.url("/x"))
		}
		if len(res.ScriptLog) != 1 || res.ScriptLog[0].Console[0].Text != "logged" {
			h.t.Errorf("response %d lost its script log: %+v", res.ID, res.ScriptLog)
		}
	}

	rows, err := h.db.Query(`SELECT s.name, rs.enabled FROM request_scripts rs
		JOIN scripts s ON s.id = rs.script_id WHERE rs.request_id = ? ORDER BY rs.run_order, rs.id`, id)
	if err != nil {
		h.t.Fatal(err)
	}
	var scripts []string
	for rows.Next() {
		var name string
		var enabled bool
		_ = rows.Scan(&name, &enabled)
		if !enabled {
			name += " (off)"
		}
		scripts = append(scripts, name)
	}
	rows.Close()
	if len(scripts) != 2 || scripts[0] != "logs" || scripts[1] != "off (off)" {
		h.t.Errorf("scripts = %v, want [logs, off (off)]", scripts)
	}

	var color string
	if err := h.db.QueryRow(`SELECT COALESCE(user_color, '') FROM favourite_items WHERE http_request_id = ?`, id).
		Scan(&color); err != nil || color != "secondary" {
		h.t.Errorf("favourite colour = %q (%v), want secondary", color, err)
	}
}

func (h *harness) binned() []models.HttpBinnedRequestSummary {
	h.t.Helper()
	var list []models.HttpBinnedRequestSummary
	h.run("bin.list", map[string]any{"project_id": h.projectID}, &list)
	return list
}

func (h *harness) undo() {
	h.t.Helper()
	if _, err := h.runner.Undo(); err != nil {
		h.t.Fatalf("undo: %v", err)
	}
}

func TestUndoingADeleteBringsTheRequestBackWhole(t *testing.T) {
	h := newHarness(t)
	id := deletable(h)

	h.run("request.delete", map[string]any{"ids": []int64{id}}, nil)
	if n := len(h.binned()); n != 1 {
		t.Fatalf("bin = %d requests after the delete, want 1", n)
	}

	h.undo()

	if n := len(h.binned()); n != 0 {
		t.Errorf("bin = %d requests after undo, want 0", n)
	}
	checkComplete(h, id)
}

func TestRestoringFromTheBinBringsTheRequestBackWhole(t *testing.T) {
	h := newHarness(t)
	id := deletable(h)
	h.run("request.delete", map[string]any{"ids": []int64{id}}, nil)
	h.run("request.delete", map[string]any{"ids": []int64{h.create(models.HttpRequest{Method: "GET", URL: h.url("/y")})}}, nil)

	bin := h.binned()
	if len(bin) != 2 {
		t.Fatalf("bin = %+v", bin)
	}
	var binID int64
	for _, b := range bin {
		if b.OriginalRequestID == id {
			binID = b.ID
		}
	}
	var restored []models.HttpRequest
	h.run("bin.restore", map[string]any{"items": []map[string]int64{{"bin_id": binID, "collection_id": h.collectionID}}}, &restored)

	if len(restored) != 1 || len(h.binned()) != 1 {
		t.Fatalf("restored %+v, %d left in the bin", restored, len(h.binned()))
	}
	checkComplete(h, restored[0].ID)

	h.undo()
	if n := len(h.binned()); n != 2 {
		t.Errorf("bin = %d after undoing the restore, want 2", n)
	}
	var left int
	_ = h.db.QueryRow(`SELECT COUNT(*) FROM http_requests WHERE id = ?`, restored[0].ID).Scan(&left)
	if left != 0 {
		t.Error("undoing the restore left the restored request behind")
	}
}

func TestDeletingForeverEmptiesTheBin(t *testing.T) {
	h := newHarness(t)
	id := deletable(h)
	h.run("request.delete", map[string]any{"ids": []int64{id}}, nil)

	h.run("bin.delete_permanently", map[string]any{"bin_ids": []int64{h.binned()[0].ID}}, nil)

	if n := len(h.binned()); n != 0 {
		t.Errorf("bin = %d after deleting forever", n)
	}
	var responses int
	_ = h.db.QueryRow(`SELECT COUNT(*) FROM responses WHERE request_id = ?`, id).Scan(&responses)
	if responses != 0 {
		t.Errorf("%d responses survived", responses)
	}
}
