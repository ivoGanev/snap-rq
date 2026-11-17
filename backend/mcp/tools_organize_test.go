package mcp

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"snap-rq/backend/commands"
)

// fakeRunner records the commands a tool runs and replays canned results.
type fakeRunner struct {
	calls   []fakeCall
	results map[string]any
	failOn  map[string]error // command -> error, once the call count for it reaches failAt
	failAt  map[string]int
	count   map[string]int
}

type fakeCall struct {
	command string
	input   map[string]any
}

func (f *fakeRunner) Run(name string, input json.RawMessage) (any, error) {
	var in map[string]any
	_ = json.Unmarshal(input, &in)
	f.calls = append(f.calls, fakeCall{name, in})
	if f.count == nil {
		f.count = map[string]int{}
	}
	f.count[name]++
	if err, ok := f.failOn[name]; ok && f.count[name] >= f.failAt[name] {
		return nil, err
	}
	return f.results[name], nil
}

func (f *fakeRunner) ListCommands() []commands.CommandInfo { return nil }

func newFakeServer(f *fakeRunner) *Server { return NewServer(f, nil, "test") }

func TestCreateRequestDefaultsMethodToGET(t *testing.T) {
	f := &fakeRunner{}
	if _, err := createRequest(newFakeServer(f), createRequestInput{CollectionID: 3, Name: "a", URL: "http://x"}); err != nil {
		t.Fatal(err)
	}
	if got := f.calls[0].input["method"]; got != "GET" {
		t.Fatalf("method = %v, want GET", got)
	}
	f = &fakeRunner{}
	_, _ = createRequest(newFakeServer(f), createRequestInput{CollectionID: 3, Method: " post "})
	if got := f.calls[0].input["method"]; got != "POST" {
		t.Fatalf("method = %v, want POST", got)
	}
}

func TestCreateRequestNeedsACollection(t *testing.T) {
	f := &fakeRunner{}
	if _, err := createRequest(newFakeServer(f), createRequestInput{Name: "a"}); err == nil {
		t.Fatal("want an error")
	}
	if len(f.calls) != 0 {
		t.Fatal("must not run a command on invalid input")
	}
}

func TestMoveRequestsReadsThenWritesTheWholeRequest(t *testing.T) {
	f := &fakeRunner{results: map[string]any{
		"request.get": map[string]any{"id": 7, "collection_id": 1, "name": "keep me", "url": "http://u", "body": "{}", "request_headers": "A: b"},
	}}
	out, err := moveRequests(newFakeServer(f), moveRequestsInput{IDs: []int64{7}, CollectionID: 9})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 2 || f.calls[0].command != "request.get" || f.calls[1].command != "request.update" {
		t.Fatalf("calls = %+v", f.calls)
	}
	update := f.calls[1].input
	if update["collection_id"] != float64(9) {
		t.Errorf("collection_id = %v", update["collection_id"])
	}
	for _, field := range []string{"name", "url", "body", "request_headers"} {
		if update[field] == nil || update[field] == "" {
			t.Errorf("field %s was lost: update would blank it", field)
		}
	}
	if raw, _ := json.Marshal(out); !strings.Contains(string(raw), `"moved":[7]`) {
		t.Errorf("result = %s", raw)
	}
}

func TestMoveRequestsReportsWhatAlreadyMoved(t *testing.T) {
	f := &fakeRunner{
		results: map[string]any{"request.get": map[string]any{"id": 1, "collection_id": 1}},
		failOn:  map[string]error{"request.update": errors.New("boom")},
		failAt:  map[string]int{"request.update": 2},
	}
	_, err := moveRequests(newFakeServer(f), moveRequestsInput{IDs: []int64{1, 2, 3}, CollectionID: 9})
	if err == nil || !strings.Contains(err.Error(), "request 2 was not moved") || !strings.Contains(err.Error(), "[1]") {
		t.Fatalf("error = %v", err)
	}
	if f.count["request.get"] != 2 {
		t.Fatalf("must stop at the first failure, got %d reads", f.count["request.get"])
	}
}

func TestMoveRequestsRejectsEmptyInput(t *testing.T) {
	f := &fakeRunner{}
	s := newFakeServer(f)
	if _, err := moveRequests(s, moveRequestsInput{CollectionID: 1}); err == nil {
		t.Error("no ids must fail")
	}
	if _, err := moveRequests(s, moveRequestsInput{IDs: []int64{1}}); err == nil {
		t.Error("no collection must fail")
	}
	if len(f.calls) != 0 {
		t.Error("must not run commands on invalid input")
	}
}

func TestCappedJSONRefusesHugeResults(t *testing.T) {
	if _, err := cappedJSON(strings.Repeat("x", maxOutputBytes+1)); err == nil {
		t.Fatal("want an error over the cap")
	}
	if _, err := cappedJSON("small"); err != nil {
		t.Fatal(err)
	}
}

func TestEveryCatalogToolHasABindingAndViceVersa(t *testing.T) {
	if err := validateBindings(); err != nil {
		t.Fatal(err)
	}
}
