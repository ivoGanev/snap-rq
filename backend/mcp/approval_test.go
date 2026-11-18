package mcp

import (
	"context"
	"strings"
	"testing"
	"time"
)

type fakeApprover struct {
	answer bool
	asked  []ApprovalRequest
}

func (f *fakeApprover) Ask(_ context.Context, req ApprovalRequest) (bool, error) {
	f.asked = append(f.asked, req)
	return f.answer, nil
}

func serverWith(f *fakeRunner, a Approver, require bool) *Server {
	s := NewServer(f, nil, "t")
	s.approver = a
	s.requireApproval = require
	return s
}

func TestGateOnlyAsksForRiskyTools(t *testing.T) {
	a := &fakeApprover{answer: true}
	s := serverWith(&fakeRunner{}, a, true)
	if err := s.gate(context.Background(), ToolMeta{Name: "move_requests"}, "c", moveRequestsInput{}); err != nil || len(a.asked) != 0 {
		t.Fatalf("a safe tool must not ask: %v %d", err, len(a.asked))
	}
	if err := s.gate(context.Background(), ToolMeta{Name: "delete_requests"}, "c", deleteRequestsInput{IDs: []int64{1}}); err != nil || len(a.asked) != 1 {
		t.Fatalf("a risky tool must ask once: %v %d", err, len(a.asked))
	}
}

func TestGateRefusesWhenTheUserSaysNo(t *testing.T) {
	s := serverWith(&fakeRunner{}, &fakeApprover{answer: false}, true)
	if err := s.gate(context.Background(), ToolMeta{Name: "edit_request_content"}, "c", contentEditInput{IDs: []int64{1}, Field: "url"}); err != ErrNotApproved {
		t.Fatalf("err = %v", err)
	}
}

func TestGateFailsClosedWithoutAnApprover(t *testing.T) {
	s := serverWith(&fakeRunner{}, nil, true)
	if err := s.gate(context.Background(), ToolMeta{Name: "delete_requests"}, "c", deleteRequestsInput{}); err != ErrNotApproved {
		t.Fatalf("err = %v", err)
	}
}

func TestGateSkippedWhenAskingIsSwitchedOff(t *testing.T) {
	a := &fakeApprover{}
	s := serverWith(&fakeRunner{}, a, false)
	if err := s.gate(context.Background(), ToolMeta{Name: "delete_requests"}, "c", deleteRequestsInput{}); err != nil || len(a.asked) != 0 {
		t.Fatalf("%v %d", err, len(a.asked))
	}
}

func TestDialogWarnsWhenALoginWouldMoveToAnotherHost(t *testing.T) {
	f := &fakeRunner{results: map[string]any{"request.get": map[string]any{
		"name": "login", "url": "https://api.example.com/v1/me",
		"request_headers": "Authorization: Bearer abc\nAccept: */*",
	}}}
	a := &fakeApprover{answer: true}
	s := serverWith(f, a, true)
	in := contentEditInput{IDs: []int64{1, 2, 3}, Field: "url", Value: "https://evil.example/x?token=zzz"}
	if err := s.gate(context.Background(), ToolMeta{Name: "edit_request_content"}, "claude-code 2.0", in); err != nil {
		t.Fatal(err)
	}
	req := a.asked[0]
	all := req.Title + " " + strings.Join(req.Lines, " ") + " " + strings.Join(req.Warnings, " ")
	for _, want := range []string{"claude-code 2.0", "3 requests", "address (URL)", "api.example.com", "evil.example"} {
		if !strings.Contains(all, want) {
			t.Errorf("dialog lacks %q: %s", want, all)
		}
	}
	if len(req.Warnings) != 1 || !strings.Contains(req.Warnings[0], "3 of these requests send a login") {
		t.Errorf("warnings = %v", req.Warnings)
	}
	if strings.Contains(all, "zzz") || strings.Contains(all, "Bearer abc") {
		t.Errorf("the dialog leaks a secret: %s", all)
	}
}

func TestDialogForDeleteListsNames(t *testing.T) {
	f := &fakeRunner{results: map[string]any{"request.get": map[string]any{"name": "old login"}}}
	a := &fakeApprover{answer: true}
	s := serverWith(f, a, true)
	_ = s.gate(context.Background(), ToolMeta{Name: "delete_requests"}, "", deleteRequestsInput{IDs: []int64{4}})
	req := a.asked[0]
	if !strings.Contains(req.Title, "An AI client wants to move 1 request to the trash") || !strings.Contains(strings.Join(req.Lines, " "), "old login") {
		t.Fatalf("%+v", req)
	}
}

// The person reading the dialog has no other context, so it says where the
// requests live, read from the real data.
func TestDialogSaysWhichProjectAndCollection(t *testing.T) {
	f := &fakeRunner{results: map[string]any{
		"request.get":     map[string]any{"name": "old login", "collection_id": 7, "project_id": 2},
		"project.list":    []map[string]any{{"id": 1, "name": "Other"}, {"id": 2, "name": "Shop API"}},
		"collection.list": []map[string]any{{"id": 7, "name": "Checkout"}},
	}}
	a := &fakeApprover{answer: true}
	s := serverWith(f, a, true)
	_ = s.gate(context.Background(), ToolMeta{Name: "delete_requests"}, "", deleteRequestsInput{IDs: []int64{4}})
	lines := strings.Join(a.asked[0].Lines, "\n")
	if !strings.Contains(lines, "Shop API › Checkout: old login") {
		t.Fatalf("dialog lacks the location: %s", lines)
	}

	// When it cannot be found, it says so instead of guessing.
	f = &fakeRunner{results: map[string]any{"request.get": map[string]any{"name": "x", "collection_id": 9, "project_id": 9}}}
	a = &fakeApprover{answer: true}
	s = serverWith(f, a, true)
	_ = s.gate(context.Background(), ToolMeta{Name: "delete_requests"}, "", deleteRequestsInput{IDs: []int64{4}})
	if !strings.Contains(strings.Join(a.asked[0].Lines, "\n"), "Location unknown: x") {
		t.Fatalf("lines = %v", a.asked[0].Lines)
	}
}

func TestHostOf(t *testing.T) {
	for in, want := range map[string]string{
		"https://api.example.com/v1?x=1": "api.example.com",
		"http://user:pw@host:8080/p":     "host:8080",
		"{{base_url}}/users":             "{{base_url}}",
		"":                               "(empty)",
	} {
		if got := hostOf(in); got != want {
			t.Errorf("hostOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBrokerAnswersAndWaitsAsLongAsTheClientDoes(t *testing.T) {
	b := NewBroker()
	events := make(chan string, 8)
	b.SetEmit(func(name string, data any) {
		events <- name
		if req, ok := data.(ApprovalRequest); ok {
			go func() {
				time.Sleep(150 * time.Millisecond) // there is no timeout: a slow answer still counts
				b.Respond(req.ID, true)
			}()
		}
	})
	allowed, err := b.Ask(context.Background(), ApprovalRequest{Tool: "t"})
	if err != nil || !allowed {
		t.Fatalf("allowed=%v err=%v", allowed, err)
	}
	if got := []string{<-events, <-events}; got[0] != "mcp:approval" || got[1] != "mcp:approval-closed" {
		t.Fatalf("events = %v", got)
	}
	if len(b.Pending()) != 0 {
		t.Fatal("nothing should be left waiting")
	}
}

func TestBrokerWithdrawsTheDialogWhenTheClientGivesUp(t *testing.T) {
	b := NewBroker()
	closed := make(chan struct{})
	b.SetEmit(func(name string, _ any) {
		if name == "mcp:approval-closed" {
			close(closed)
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	allowed, err := b.Ask(ctx, ApprovalRequest{Tool: "t"})
	if allowed || err == nil {
		t.Fatalf("allowed=%v err=%v", allowed, err)
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("the dialog was not withdrawn")
	}
	if len(b.Pending()) != 0 {
		t.Fatal("nothing should be left waiting")
	}
}

func TestBrokerRefusesWhenThereIsNoWindow(t *testing.T) {
	b := NewBroker()
	if allowed, err := b.Ask(context.Background(), ApprovalRequest{Tool: "t"}); allowed || err != nil {
		t.Fatalf("allowed=%v err=%v", allowed, err)
	}
	if b.Respond("nope", true) {
		t.Fatal("answering an unknown request must report false")
	}
}

func TestGateReportsACancelledClient(t *testing.T) {
	b := NewBroker()
	b.SetEmit(func(string, any) {})
	s := serverWith(&fakeRunner{}, b, true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := s.gate(ctx, ToolMeta{Name: "delete_requests"}, "c", deleteRequestsInput{})
	if err == nil || err == ErrNotApproved || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("err = %v", err)
	}
}
