package mcp

import (
	"strings"
	"testing"
)

func TestListVariablesNeverReturnsValues(t *testing.T) {
	f := &fakeRunner{results: map[string]any{"variable.list": []map[string]any{
		{"id": 1, "environment_id": 2, "key": "api_key", "value": "sk-live-SECRET"},
		{"id": 2, "environment_id": 2, "key": "base_url", "value": "https://prod.example.com"},
	}}}
	out, err := listVariables(newFakeServer(f), environmentInput{EnvironmentID: 2})
	if err != nil {
		t.Fatal(err)
	}
	text, err := cappedJSON(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "SECRET") || strings.Contains(text, "prod.example") || strings.Contains(text, "value") {
		t.Fatalf("a value leaked: %s", text)
	}
	if !strings.Contains(text, "api_key") || !strings.Contains(text, "base_url") {
		t.Fatalf("names missing: %s", text)
	}
	if f.calls[0].command != "variable.list" {
		t.Fatalf("ran %q", f.calls[0].command)
	}
}
