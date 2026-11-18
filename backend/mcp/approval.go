package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Risky tools wait for the user to approve them in SnapRQ. The server sends an
// event to the window, the window shows a dialog, and the answer comes back
// through SettingsService.RespondApproval. The call waits for as long as the
// client keeps it open: there is no timeout, and a client that gives up
// withdraws the request. See docs/mcp.md.

// approvalTools are the tools that ask first while "ask before risky changes"
// is on. It is data, like the catalog, not a user setting.
var approvalTools = map[string]bool{
	"edit_request_content": true,
	"delete_requests":      true,
}

// ErrNotApproved is returned to the client when the user said no.
var ErrNotApproved = errors.New("the user did not approve this in SnapRQ")

// approvalMaxLooked is how many requests are read to describe a change.
const approvalMaxLooked = 25

// ApprovalRequest is what the dialog shows.
type ApprovalRequest struct {
	ID       string   `json:"id"`
	Tool     string   `json:"tool"`
	Client   string   `json:"client"`
	Title    string   `json:"title"`
	Lines    []string `json:"lines"`
	Warnings []string `json:"warnings"`
	AskedAt  int64    `json:"asked_at"` // unix milliseconds
}

// Approver asks the user. It is an interface so tests can answer for them.
type Approver interface {
	Ask(ctx context.Context, req ApprovalRequest) (bool, error)
}

// Broker is the real Approver: it announces requests as events and waits.
type Broker struct {
	mu      sync.Mutex
	pending map[string]*pendingApproval
	emit    func(name string, data any)
}

type pendingApproval struct {
	req    ApprovalRequest
	answer chan bool
}

// NewBroker returns a broker with nothing waiting.
func NewBroker() *Broker {
	return &Broker{pending: map[string]*pendingApproval{}}
}

// SetEmit sets how events reach the window. Without it nothing can be
// approved, so every request is refused at once.
func (b *Broker) SetEmit(emit func(name string, data any)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.emit = emit
}

// Ask blocks until the user answers. It returns an error only when ctx ends
// first, which means the client gave up; the dialog is then withdrawn.
func (b *Broker) Ask(ctx context.Context, req ApprovalRequest) (bool, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return false, err
	}
	req.ID = hex.EncodeToString(raw[:])
	req.AskedAt = time.Now().UnixMilli()
	p := &pendingApproval{req: req, answer: make(chan bool, 1)}

	b.mu.Lock()
	b.pending[req.ID] = p
	emit := b.emit
	b.mu.Unlock()
	if emit == nil {
		b.finish(req.ID)
		return false, nil
	}
	emit("mcp:approval", req)

	allowed := false
	var err error
	select {
	case allowed = <-p.answer:
	case <-ctx.Done():
		err = ctx.Err()
	}
	b.finish(req.ID)
	emit("mcp:approval-closed", req.ID)
	return allowed, err
}

func (b *Broker) finish(id string) {
	b.mu.Lock()
	delete(b.pending, id)
	b.mu.Unlock()
}

// Respond delivers the user's answer; it reports whether the request was
// still waiting.
func (b *Broker) Respond(id string, allow bool) bool {
	b.mu.Lock()
	p, ok := b.pending[id]
	b.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case p.answer <- allow:
		return true
	default:
		return false
	}
}

// Pending lists the requests still waiting, oldest first, so a window that
// was reloaded can show them again.
func (b *Broker) Pending() []ApprovalRequest {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]ApprovalRequest, 0, len(b.pending))
	for _, p := range b.pending {
		out = append(out, p.req)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AskedAt < out[j].AskedAt })
	return out
}

// gate asks the user when the tool is risky and asking is switched on. With
// no approver it refuses: it fails closed.
func (s *Server) gate(ctx context.Context, m ToolMeta, client string, in any) error {
	if !approvalTools[m.Name] || !s.approvalRequired() {
		return nil
	}
	if s.approver == nil {
		return ErrNotApproved
	}
	allowed, err := s.approver.Ask(ctx, s.describe(m, client, in))
	if err != nil {
		return fmt.Errorf("the request was cancelled before the user answered: %w", err)
	}
	if !allowed {
		return ErrNotApproved
	}
	return nil
}

func (s *Server) approvalRequired() bool {
	s.maskMu.RLock()
	defer s.maskMu.RUnlock()
	return s.requireApproval
}

var fieldLabels = map[string]string{
	"url": "address (URL)", "method": "method", "headers": "headers",
	"body": "body", "body_type": "body type", "body_form": "form body",
}

// describe says in plain words what the call would do.
func (s *Server) describe(m ToolMeta, client string, in any) ApprovalRequest {
	who := strings.TrimSpace(client)
	if who == "" {
		who = "An AI client"
	}
	req := ApprovalRequest{Tool: m.Name, Client: client, Lines: []string{}, Warnings: []string{}}
	masker := s.currentAuditMasker()
	switch v := in.(type) {
	case contentEditInput:
		s.describeContentEdit(&req, who, v, masker)
	case deleteRequestsInput:
		s.describeDelete(&req, who, v)
	default:
		req.Title = who + " wants to run " + m.Name
	}
	return req
}

func (s *Server) describeContentEdit(req *ApprovalRequest, who string, in contentEditInput, masker *Masker) {
	n := len(in.IDs)
	label := fieldLabels[in.Field]
	if label == "" {
		label = in.Field
	}
	req.Title = fmt.Sprintf("%s wants to change the %s of %s", who, label, plural(n, "request"))
	infos := s.lookRequests(in.IDs)
	newValue := valueText(in.Value)

	switch in.Field {
	case "url":
		masked := masker.URL(newValue)
		req.Lines = append(req.Lines, "New address: "+truncate(masked, 120))
		oldHosts := distinct(infos, func(r requestInfo) string { return hostOf(r.URL) })
		if len(oldHosts) > 0 {
			req.Lines = append(req.Lines, "Now pointing at: "+listWithMore(oldHosts, 4))
		}
		newHost := hostOf(newValue)
		changed := true
		if len(oldHosts) == 1 && oldHosts[0] == newHost {
			changed = false
		}
		if changed {
			req.Lines = append(req.Lines, "The host becomes: "+newHost)
		}
		carrying := 0
		for _, r := range infos {
			if hasLoginHeader(masker, r.Headers) {
				carrying++
			}
		}
		if carrying > 0 && changed {
			req.Warnings = append(req.Warnings, fmt.Sprintf(
				"%d of these requests send a login or token. After this change it would be sent to %s.", carrying, newHost))
		}
	default:
		req.Lines = append(req.Lines, "New value: "+truncate(masker.Text(newValue), 160))
	}
	if len(infos) < n {
		req.Lines = append(req.Lines, fmt.Sprintf("Details shown for the first %d of %d requests.", len(infos), n))
	}
	req.Lines = append(req.Lines, "You can undo this with Ctrl+Z.")
}

func (s *Server) describeDelete(req *ApprovalRequest, who string, in deleteRequestsInput) {
	req.Title = fmt.Sprintf("%s wants to move %s to the trash", who, plural(len(in.IDs), "request"))
	infos := s.lookRequests(in.IDs)
	names := make([]string, 0, len(infos))
	for _, r := range infos {
		if r.Name != "" {
			names = append(names, r.Name)
		}
	}
	if len(names) > 0 {
		req.Lines = append(req.Lines, listWithMore(names, 5))
	}
	req.Lines = append(req.Lines, "They can be restored from the trash, or undone with Ctrl+Z.")
}

type requestInfo struct {
	Name, URL, Headers string
}

// lookRequests reads the real requests, so the dialog can say what is being
// changed. Failures just mean less detail.
func (s *Server) lookRequests(ids []int64) []requestInfo {
	out := []requestInfo{}
	for i, id := range ids {
		if i == approvalMaxLooked {
			break
		}
		row, err := s.runMap("request.get", map[string]any{"id": id})
		if err != nil {
			continue
		}
		info := requestInfo{}
		info.Name, _ = row["name"].(string)
		info.URL, _ = row["url"].(string)
		info.Headers, _ = row["request_headers"].(string)
		out = append(out, info)
	}
	return out
}

func hasLoginHeader(m *Masker, headers string) bool {
	for _, line := range strings.Split(headers, "\n") {
		if name, _, ok := strings.Cut(line, ":"); ok && m.sensitive(name) {
			return true
		}
	}
	return false
}

// hostOf returns the host part of a URL, tolerating {{placeholders}}.
func hostOf(raw string) string {
	s := strings.TrimSpace(raw)
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, "@"); i >= 0 {
		s = s[i+1:]
	}
	if s == "" {
		return "(empty)"
	}
	return s
}

func valueText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	out, err := toGeneric(v)
	if err != nil {
		return ""
	}
	return fmt.Sprint(out)
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func distinct(infos []requestInfo, f func(requestInfo) string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, r := range infos {
		if v := f(r); !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func listWithMore(items []string, max int) string {
	if len(items) <= max {
		return strings.Join(items, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(items[:max], ", "), len(items)-max)
}
