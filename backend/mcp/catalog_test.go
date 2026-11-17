package mcp

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// TestExposedToolNames is the allowlist test: adding a tool means changing
// this list on purpose.
func TestExposedToolNames(t *testing.T) {
	want := []string{
		"get_request", "list_collections", "list_favourites", "list_profiles",
		"list_projects", "list_recent_requests", "list_requests", "list_responses",
		"list_tagged_requests", "list_tags", "list_trash",
	}
	var got []string
	for _, m := range Catalog {
		got = append(got, m.Name)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("exposed tools changed:\n got %v\nwant %v", got, want)
	}
}

func commandsOf(catalog []ToolMeta) []string {
	var out []string
	for _, m := range catalog {
		out = append(out, m.Command)
	}
	return out
}

func TestCatalogIsValid(t *testing.T) {
	if err := validate(Catalog, commandsOf(Catalog)); err != nil {
		t.Fatal(err)
	}
}

func TestNoToolCallsANeverExposedCommand(t *testing.T) {
	for _, m := range Catalog {
		for _, banned := range NeverExposed {
			if m.Command == banned {
				t.Errorf("tool %s calls %s", m.Name, banned)
			}
		}
	}
}

func TestPhase1ToolsAreAllRead(t *testing.T) {
	for _, m := range Catalog {
		if m.Risk != RiskRead {
			t.Errorf("%s is %s; phase 1 only ships read tools", m.Name, m.Risk)
		}
	}
}

func TestValidateRejects(t *testing.T) {
	ok := ToolMeta{"a", "does a", RiskRead, "x.a"}
	cases := map[string]struct {
		catalog    []ToolMeta
		registered []string
		wantErr    string
	}{
		"duplicate name":      {[]ToolMeta{ok, ok}, []string{"x.a"}, "twice"},
		"unknown risk":        {[]ToolMeta{{"a", "d", "scary", "x.a"}}, []string{"x.a"}, "risk class"},
		"never exposed":       {[]ToolMeta{{"a", "d", RiskRead, "settings.update"}}, []string{"settings.update"}, "never be exposed"},
		"unregistered":        {[]ToolMeta{ok}, []string{"other"}, "unregistered"},
		"missing description": {[]ToolMeta{{"a", "", RiskRead, "x.a"}}, []string{"x.a"}, "required"},
	}
	for name, tc := range cases {
		err := validate(tc.catalog, tc.registered)
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: got %v, want error containing %q", name, err, tc.wantErr)
		}
	}
}
