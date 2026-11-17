// Package mcp exposes a curated allowlist of SnapRQ commands over the Model
// Context Protocol. This file is data only: nothing here imports the SDK, so
// the allowlist rules can be tested without it. See docs/mcp.md.
package mcp

import (
	"errors"
	"fmt"
)

// Risk is a tool's risk class. Classes decide the default availability of a
// tool; the user can override any single tool in the settings.
type Risk string

const (
	RiskRead        Risk = "read"
	RiskOrganize    Risk = "organize"
	RiskDestructive Risk = "destructive"
	RiskExternal    Risk = "external"
	RiskSensitive   Risk = "sensitive"
)

func (r Risk) valid() bool {
	switch r {
	case RiskRead, RiskOrganize, RiskDestructive, RiskExternal, RiskSensitive:
		return true
	}
	return false
}

// ToolMeta describes one MCP tool and the one command it calls.
type ToolMeta struct {
	Name        string
	Description string
	Risk        Risk
	Command     string // the runner command id, e.g. "request.list_for_collection"
}

// NeverExposed lists commands no tool may call, whatever the settings.
var NeverExposed = []string{
	"settings.update",       // would let an assistant change timeouts, SSL checks, undo size
	"response.save_to_file", // arbitrary file write
	"icon.cleanup",
}

// Catalog is every tool MCP offers. Adding a tool is a deliberate edit here
// (and in the exact-names test).
var Catalog = []ToolMeta{
	{"list_profiles", "List profiles.", RiskRead, "profile.list"},
	{"list_projects", "List projects.", RiskRead, "project.list"},
	{"list_collections", "List the collections of a project.", RiskRead, "collection.list"},
	{"list_requests", "List request summaries (no bodies) in a collection, in the user's order.", RiskRead, "request.list_for_collection"},
	{"list_recent_requests", "List the most recently used requests of a project.", RiskRead, "request.list_recent"},
	{"get_request", "Get one full request: URL, method, headers, body.", RiskRead, "request.get"},
	{"list_responses", "List the saved response history of a request, newest first.", RiskRead, "response.list_for_request"},
	{"list_tags", "List the tags of a project.", RiskRead, "tag.list"},
	{"list_tagged_requests", "List the requests of a project carrying a tag.", RiskRead, "tag.list_requests"},
	{"list_favourites", "List the favourite collections of a project.", RiskRead, "favourite.list"},
	{"list_trash", "List the requests in a project's trash.", RiskRead, "bin.list"},

	// collections
	{"create_collection", "Create a collection in a project.", RiskOrganize, "collection.create"},
	{"rename_collection", "Rename a collection.", RiskOrganize, "collection.update"},

	// requests
	{"create_request", "Create a request at the end of a collection. method defaults to GET.", RiskOrganize, "request.create"},
	{"duplicate_request", "Duplicate a request.", RiskOrganize, "request.duplicate"},
	{"delete_requests", "Move requests to the trash. They can be restored with restore_from_trash or undone.", RiskOrganize, "request.delete"},
	{"restore_from_trash", "Restore requests from the trash into collections.", RiskOrganize, "bin.restore"},
	{"bulk_edit_requests", "Set one field (name, url, method, headers, body, body_type, body_form, color) on many requests. Undoable.", RiskOrganize, "request.bulk_set"},
	{"reorder_requests", "Set the custom order of the requests in a collection.", RiskOrganize, "request.update_order"},
	{"move_requests", "Move requests to another collection (appended at its end). Not undoable: move them back to revert.", RiskOrganize, "request.update"},

	// tags
	{"add_tag", "Tag a request; the tag is created if the project has none with that name.", RiskOrganize, "tag.add"},
	{"remove_tag", "Remove a tag from a request.", RiskOrganize, "tag.remove"},
	{"rename_tag", "Rename a tag everywhere in a project.", RiskOrganize, "tag.rename"},
	{"reorder_tagged_requests", "Set the custom order of the requests inside one tag.", RiskOrganize, "tag.reorder"},
	{"set_tag_request_colors", "Set or clear the colour of requests as shown inside one tag.", RiskOrganize, "tag.set_color"},

	// favourites
	{"create_favourite", "Create a favourite collection in a project.", RiskOrganize, "favourite.create"},
	{"rename_favourite", "Rename a favourite collection.", RiskOrganize, "favourite.update"},
	{"add_to_favourite", "Add a request to a favourite collection.", RiskOrganize, "favourite.add_request"},
	{"remove_from_favourite", "Remove requests from a favourite collection.", RiskOrganize, "favourite.remove_requests"},
	{"reorder_favourite_requests", "Set the custom order of the requests inside a favourite collection.", RiskOrganize, "favourite.reorder"},
	{"set_favourite_request_colors", "Set or clear the colour of requests as shown inside a favourite collection.", RiskOrganize, "favourite.set_color"},
}

// metaFor returns the catalog entry with the given name.
func metaFor(name string) (ToolMeta, bool) {
	for _, m := range Catalog {
		if m.Name == name {
			return m, true
		}
	}
	return ToolMeta{}, false
}

// ValidateCatalog checks the catalog against the commands the runner has
// registered. It runs at startup so a typo fails loudly, not at call time.
func ValidateCatalog(registered []string) error {
	return validate(Catalog, registered)
}

func validate(catalog []ToolMeta, registered []string) error {
	known := make(map[string]bool, len(registered))
	for _, c := range registered {
		known[c] = true
	}
	banned := make(map[string]bool, len(NeverExposed))
	for _, c := range NeverExposed {
		banned[c] = true
	}
	seen := map[string]bool{}
	for _, m := range catalog {
		switch {
		case m.Name == "" || m.Command == "" || m.Description == "":
			return fmt.Errorf("mcp tool %q: name, description and command are required", m.Name)
		case seen[m.Name]:
			return fmt.Errorf("mcp tool %q is listed twice", m.Name)
		case !m.Risk.valid():
			return fmt.Errorf("mcp tool %q has unknown risk class %q", m.Name, m.Risk)
		case banned[m.Command]:
			return fmt.Errorf("mcp tool %q calls %s, which must never be exposed", m.Name, m.Command)
		case !known[m.Command]:
			return fmt.Errorf("mcp tool %q calls unregistered command %s", m.Name, m.Command)
		}
		seen[m.Name] = true
	}
	return nil
}

// ErrDisabled is returned when a tool is switched off in the settings.
var ErrDisabled = errors.New("this tool is disabled in SnapRQ's MCP settings")
