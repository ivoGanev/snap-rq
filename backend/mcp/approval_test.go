package mcp

import (
	"strings"
	"testing"
	"time"
)

type fakeApprover struct {
	answer bool
	asked  []ApprovalRequest
}

func (f *fakeApprover) Ask(req ApprovalRequest) bool {
	f.asked = append(f.asked, req)
	return f.answer
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
	if err := s.gate(ToolMeta{Name: "move_requests"}, "c", moveRequestsInput{}); err != nil || len(a.asked) != 0 {
		t.Fatalf("a safe tool must not ask: %v %d", err, len(a.asked))
	}
	if err := s.gate(ToolMeta{Name: "delete_requests"}, "c", deleteRequestsInput{IDs: []int64{1}}); err != nil || len(a.asked) != 1 {
		t.Fatalf("a risky tool must ask once: %v %d", err, len(a.asked))
	}
}

func TestGateRefusesWhenTheUserSaysNo(t *testing.T) {
	s := serverWith(&fakeRunner{}, &fakeApprover{answer: false}, true)
	if err := s.gate(ToolMeta{Name: "edit_request_content"}, "c", contentEditInput{IDs: []int64{1}, Field: "url"}); err != ErrNotApproved {
		t.Fatalf("err = %v", err)
	}
}

func TestGateFailsClosedWithoutAnApprover(t *testing.T) {
	s := serverWith(&fakeRunner{}, nil, true)
	if err := s.gate(ToolMeta{Name: "delete_requests"}, "c", deleteRequestsInput{}); err != ErrNotApproved {
		t.Fatalf("err = %v", err)
	}
}

func TestGateSkippedWhenAskingIsSwitchedOff(t *testing.T) {
	a := &fakeApprover{}
	s := serverWith(&fakeRunner{}, a, false)
	if err := s.gate(ToolMeta{Name: "delete_requests"}, "c", deleteRequestsInput{}); err != nil || len(a.asked) != 0 {
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
	if err := s.gate(ToolMeta{Name: "edit_request_content"}, "claude-code 2.0", in); err != nil {
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
	_ = s.gate(ToolMeta{Name: "delete_requests"}, "", deleteRequestsInput{IDs: []int64{4}})
	req := a.asked[0]
	if !strings.Contains(req.Title, "An AI client wants to move 1 request to the trash") || !strings.Contains(strings.Join(req.Lines, " "), "old login") {
		t.Fatalf("%+v", req)
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

func TestBrokerAnswersAndTimesOut(t *testing.T) {
	b := NewBroker()
	b.timeout = 2 * time.Second
	events := make(chan string, 8)
	b.SetEmit(func(name string, data any) {
		events <- name
		if req, ok := data.(ApprovalRequest); ok {
			go b.Respond(req.ID, true)
		}
	})
	if !b.Ask(ApprovalRequest{Tool: "t"}) {
		t.Fatal("an allowed request must return true")
	}
	if got := []string{<-events, <-events}; got[0] != "mcp:approval" || got[1] != "mcp:approval-closed" {
		t.Fatalf("events = %v", got)
	}
	if len(b.Pending()) != 0 {
		t.Fatal("nothing should be left waiting")
	}

	b.SetEmit(func(string, any) {})
	b.timeout = 50 * time.Millisecond
	if b.Ask(ApprovalRequest{Tool: "t"}) {
		t.Fatal("no answer must count as no")
	}

	b.SetEmit(nil)
	if b.Ask(ApprovalRequest{Tool: "t"}) {
		t.Fatal("with no window to ask in, the answer is no")
	}
	if b.Respond("nope", true) {
		t.Fatal("answering an unknown request must report false")
	}
}
