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
		"add_tag", "add_to_favourite", "bulk_edit_requests", "create_collection", "create_favourite",
		"create_request", "delete_requests", "duplicate_request", "get_request", "list_collections",
		"list_favourites", "list_profiles", "list_projects", "list_recent_requests", "list_requests",
		"list_responses", "list_tagged_requests", "list_tags", "list_trash", "move_requests",
		"remove_from_favourite", "remove_tag", "rename_collection", "rename_favourite", "rename_tag",
		"reorder_favourite_requests", "reorder_requests", "reorder_tagged_requests",
		"restore_from_trash", "set_favourite_request_colors", "set_tag_request_colors",
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

func TestOnlyReadAndOrganizeShip(t *testing.T) {
	for _, m := range Catalog {
		if m.Risk != RiskRead && m.Risk != RiskOrganize {
			t.Errorf("%s is %s; restricted classes arrive in phase 4", m.Name, m.Risk)
		}
		isQuery := strings.HasPrefix(m.Name, "list_") || strings.HasPrefix(m.Name, "get_")
		if isQuery != (m.Risk == RiskRead) {
			t.Errorf("%s: query-named tools must be read, and only they", m.Name)
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
