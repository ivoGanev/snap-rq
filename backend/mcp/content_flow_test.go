package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// connect serves h's tools over the real SDK, as a client sees them, with
// asking switched off.
func (h harness) connect() *sdk.ClientSession {
	h.t.Helper()
	h.s.requireApproval = false
	h.s.sdk = sdk.NewServer(&sdk.Implementation{Name: "snap-rq", Version: "test"}, nil)
	h.s.registered = map[string]bool{}
	h.s.syncToolsLocked()

	ctx := context.Background()
	clientTransport, serverTransport := sdk.NewInMemoryTransports()
	if _, err := h.s.sdk.Connect(ctx, serverTransport, nil); err != nil {
		h.t.Fatal(err)
	}
	session, err := sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "1"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { session.Close() })
	return session
}

// callTool returns the tool's text, or its error message.
func callTool(t *testing.T, session *sdk.ClientSession, name string, args any) (string, bool) {
	t.Helper()
	res, err := session.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return err.Error(), false
	}
	var text strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			text.WriteString(tc.Text)
		}
	}
	return text.String(), !res.IsError
}

func TestContentToolsOverTheRealProtocol(t *testing.T) {
	h := newHarness(t)
	session := h.connect()
	project := h.firstID(h.run("project.list", noInput{}))
	collection := h.id(h.run("collection.create", createCollectionInput{ProjectID: project, Name: "Fleet"}))

	out, ok := callTool(t, session, "create_requests", map[string]any{"requests": []map[string]any{
		{"collection_id": collection, "name": "one", "method": "post", "url": "{{base}}/devices",
			"request_headers": "X-Team: fleet", "body": `{"networkDeviceId": "dev-exc-1", "priority": 5}`},
		{"collection_id": collection, "name": "two"},
	}})
	if !ok {
		t.Fatalf("create_requests: %s", out)
	}
	var created []struct {
		ID             int64  `json:"id"`
		Name           string `json:"name"`
		Method         string `json:"method"`
		Body           string `json:"body"`
		RequestHeaders string `json:"request_headers"`
	}
	if err := json.Unmarshal([]byte(out), &created); err != nil || len(created) != 2 {
		t.Fatalf("create_requests returned %s (%v)", out, err)
	}
	if created[0].Name != "one" || created[0].Method != "POST" || created[0].RequestHeaders != "X-Team: fleet" ||
		created[0].Body != `{"networkDeviceId": "dev-exc-1", "priority": 5}` || created[1].Name != "two" {
		t.Errorf("created %+v", created)
	}

	// The schema says value is a string, so an object is refused before
	// anything runs, with a message about the value rather than SQL.
	out, ok = callTool(t, session, "edit_request_content", map[string]any{"changes": []map[string]any{
		{"id": created[0].ID, "field": "url", "value": "http://changed"},
		{"id": created[1].ID, "field": "body", "value": map[string]any{"priority": 5}},
	}})
	if ok || strings.Contains(strings.ToLower(out), "sql") || !strings.Contains(out, "value") {
		t.Fatalf("an object value gave ok=%v: %s", ok, out)
	}
	if got := h.field(created[0].ID, "url"); got != `"{{base}}/devices"` {
		t.Errorf("url = %s after a refused call", got)
	}

	out, ok = callTool(t, session, "edit_request_content", map[string]any{"changes": []map[string]any{
		{"id": created[0].ID, "field": "body", "value": `{"priority": 6}`},
		{"id": created[1].ID, "field": "request_headers", "value": "Accept: */*"},
	}})
	if !ok || !strings.Contains(out, `\"priority\": 6`) || !strings.Contains(out, "Accept: */*") {
		t.Fatalf("edit_request_content: ok=%v %s, want the updated requests", ok, out)
	}

	out, ok = callTool(t, session, "bulk_edit_requests", map[string]any{"ids": []int64{created[1].ID}, "field": "name", "value": "renamed"})
	if !ok || !strings.Contains(out, "updated") {
		t.Fatalf("bulk_edit_requests: ok=%v %s, want the ids changed", ok, out)
	}
}
