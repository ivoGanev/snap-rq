package requests_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"snap-rq/backend/commands"
	"snap-rq/backend/commands/catalog"
	"snap-rq/backend/commands/requests"
	"snap-rq/backend/database"
	"snap-rq/backend/models"
	"snap-rq/backend/secrets"
)

// harness sends requests the way the app does: the real database, the
// commands, and request.send, against a local server that records what
// reached it.
type harness struct {
	t            *testing.T
	db           *sql.DB
	runner       *commands.Runner
	server       *httptest.Server
	projectID    int64
	collectionID int64

	mu       sync.Mutex
	received []received
}

type received struct {
	Method string
	Path   string
	Query  string
	Header http.Header
	Body   []byte
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	secrets.SetDefault(secrets.NewVault(secrets.MemoryKeyStore()))
	db, err := database.OpenPath(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	runner := commands.NewRunner(db)
	if err := runner.Register(catalog.All...); err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, db: db, runner: runner}
	if err := db.QueryRow(`SELECT id, project_id FROM collections ORDER BY id LIMIT 1`).
		Scan(&h.collectionID, &h.projectID); err != nil {
		t.Fatal(err)
	}
	h.server = httptest.NewServer(h.routes())
	t.Cleanup(h.server.Close)
	return h
}

func (h *harness) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		h.mu.Lock()
		h.received = append(h.received, received{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Clone(), body})
		h.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"method":"` + r.Method + `"}`))
	})
	mux.HandleFunc("/moved", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/landed?from=moved", http.StatusFound)
	})
	mux.HandleFunc("/status/", func(w http.ResponseWriter, r *http.Request) {
		code, _ := strconv.Atoi(r.URL.Path[len("/status/"):])
		w.WriteHeader(code)
	})
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		ms, _ := strconv.Atoi(r.URL.Query().Get("ms"))
		select {
		case <-time.After(time.Duration(ms) * time.Millisecond):
			_, _ = w.Write([]byte("slow done"))
		case <-r.Context().Done():
		}
	})
	return mux
}

func (h *harness) url(path string) string { return h.server.URL + path }

// requests returns what reached the server's catch-all route, in order.
func (h *harness) requests() []received {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]received(nil), h.received...)
}

func (h *harness) last() received {
	h.t.Helper()
	all := h.requests()
	if len(all) == 0 {
		h.t.Fatal("nothing reached the server")
	}
	return all[len(all)-1]
}

func (h *harness) run(name string, in, out any) {
	h.t.Helper()
	raw, _ := json.Marshal(in)
	result, err := h.runner.Run(name, raw)
	if err != nil {
		h.t.Fatalf("%s: %v", name, err)
	}
	if out != nil {
		encoded, _ := json.Marshal(result)
		if err := json.Unmarshal(encoded, out); err != nil {
			h.t.Fatalf("%s result %s: %v", name, encoded, err)
		}
	}
}

func (h *harness) create(req models.HttpRequest) int64 {
	h.t.Helper()
	if req.CollectionID == 0 {
		req.CollectionID = h.collectionID
	}
	if req.Name == "" {
		req.Name = req.Method + " " + req.URL
	}
	var created models.HttpRequest
	h.run("request.create", req, &created)
	return created.ID
}

// attach adds a script, in run order after the request's others.
func (h *harness) attach(requestID int64, name, source string) {
	h.t.Helper()
	h.run("script.attach", map[string]any{"request_id": requestID, "project_id": h.projectID, "script_name": name}, nil)
	var scriptID int64
	if err := h.db.QueryRow(`SELECT id FROM scripts WHERE name = ?`, name).Scan(&scriptID); err != nil {
		h.t.Fatal(err)
	}
	h.run("script.set_content", map[string]any{"id": scriptID, "content": source}, nil)
}

// environment creates one with the given plain and secret variables.
func (h *harness) environment(plain, secret map[string]string) int64 {
	h.t.Helper()
	var env models.Environment
	h.run("environment.create", map[string]any{"project_id": h.projectID, "name": "env"}, &env)
	for key, value := range plain {
		h.run("variable.create", map[string]any{"environment_id": env.ID, "key": key, "value": value}, nil)
	}
	for key, value := range secret {
		var v models.EnvironmentVariable
		h.run("variable.create", map[string]any{"environment_id": env.ID, "key": key, "secret": true}, &v)
		h.run("variable.update", map[string]any{"id": v.ID, "key": key, "value": value}, nil)
	}
	return env.ID
}

type sendOptions struct {
	environmentID int64
	plain         map[string]string
	ctx           context.Context
}

// send sends a stored request as the frontend does: the request as loaded
// and the environment's id. Plain values without an environment get one.
func (h *harness) send(requestID int64, opts sendOptions) (models.HttpResponse, error) {
	h.t.Helper()
	var req models.HttpRequest
	h.run("request.get", map[string]any{"id": requestID}, &req)
	environmentID := opts.environmentID
	if environmentID == 0 && len(opts.plain) > 0 {
		environmentID = h.environment(opts.plain, nil)
	}
	raw, _ := json.Marshal(map[string]any{"request": req, "environment_id": environmentID})
	ctx := opts.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	var result any
	var sendErr error
	requests.NewSend(h.db).Run(raw, commands.Observer{
		Update:  func(any) {},
		Finish:  func(res any, err error) { result, sendErr = res, err },
		Context: ctx,
	})
	var res models.HttpResponse
	if result != nil {
		encoded, _ := json.Marshal(result)
		_ = json.Unmarshal(encoded, &res)
	}
	return res, sendErr
}

func (h *harness) mustSend(requestID int64, opts sendOptions) models.HttpResponse {
	h.t.Helper()
	res, err := h.send(requestID, opts)
	if err != nil {
		h.t.Fatalf("send: %v", err)
	}
	return res
}

// history is the request's saved responses, newest first.
func (h *harness) history(requestID int64) []models.HttpResponse {
	h.t.Helper()
	var list []models.HttpResponse
	h.run("response.list_for_request", map[string]any{"request_id": requestID}, &list)
	return list
}
