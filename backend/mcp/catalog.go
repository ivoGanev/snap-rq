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
	// RiskContent covers changing what a request sends (URL, method, headers,
	// body). A client that can do this could redirect your credentials, so it
	// is off by default.
	RiskContent Risk = "content"
)

func (r Risk) valid() bool {
	switch r {
	case RiskRead, RiskOrganize, RiskDestructive, RiskExternal, RiskSensitive, RiskContent:
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
	"openapi.export",        // arbitrary file write
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
	{"list_scripts", "List the scripts of a project (names only; scripts cannot run yet).", RiskRead, "script.list"},
	{"list_script_requests", "List the requests a script is attached to, in the user's order.", RiskRead, "script.list_requests"},
	{"list_favourites", "List the favourite collections of a project.", RiskRead, "favourite.list"},
	{"list_trash", "List the requests in a project's trash.", RiskRead, "bin.list"},
	{"list_environments", "List the environments of a project.", RiskRead, "environment.list"},
	{"list_variables", "List the variable NAMES of an environment. Values are never returned; refer to a variable as {{name}}.", RiskRead, "variable.list"},

	// collections
	{"create_collection", "Create a collection in a project.", RiskOrganize, "collection.create"},
	{"rename_collection", "Rename a collection.", RiskOrganize, "collection.update"},

	// requests
	{"create_request", "Create an empty request (name and method only) at the end of a collection. method defaults to GET. To create requests with a URL, headers or body, use create_requests.", RiskOrganize, "request.create"},
	{"duplicate_request", "Duplicate a request.", RiskOrganize, "request.duplicate"},
	{"delete_requests", "Move requests to the trash. They can be restored with restore_from_trash or undone.", RiskOrganize, "request.delete"},
	{"restore_from_trash", "Restore requests from the trash into collections.", RiskOrganize, "bin.restore"},
	{"bulk_edit_requests", "Set name or color on many requests. Undoable. Returns the ids changed.", RiskOrganize, "request.bulk_set"},
	{"reorder_requests", "Set the order of the requests in a collection: ordered_ids lists every request id of the collection, in the wanted order. Undoable.", RiskOrganize, "request.update_order"},
	{"move_requests", "Move requests to another collection (appended at its end). Not undoable: move them back to revert.", RiskOrganize, "request.update"},

	// content: what a request sends. Off by default.
	{"create_requests", "Create one or more requests with what they send (url, method, request_headers, body, body_type, body_form), each appended to its collection in the order given. Every value is a string: send a JSON body as its text. All or nothing, one undo step; returns the created requests. The user is asked to approve each call.", RiskContent, "request.create_many"},
	{"edit_request_content", "Change what requests send, as a list of {id, field, value} changes; field is one of url, method, request_headers, body, body_type, body_form, and value is always a string (send a JSON body as its text). All or nothing, one undo step; returns the updated requests. The user is asked to approve each call.", RiskContent, "request.edit_many"},

	// scripts
	{"attach_script", "Attach a script to a request; the script is created (empty) if the project has none with that name.", RiskOrganize, "script.attach"},
	{"detach_script", "Detach a script from a request; the script itself stays.", RiskOrganize, "script.detach"},
	{"rename_script", "Rename a script. Fails if the project already has a script with the new name.", RiskOrganize, "script.rename"},
	{"reorder_script_requests", "Set the custom order of the requests a script is attached to.", RiskOrganize, "script.reorder"},
	{"set_script_request_colors", "Set or clear the colour of requests as shown inside one script.", RiskOrganize, "script.set_color"},

	// favourites
	{"create_favourite", "Create a favourite collection in a project.", RiskOrganize, "favourite.create"},
	{"rename_favourite", "Rename a favourite collection.", RiskOrganize, "favourite.update"},
	{"add_to_favourite", "Add a request to a favourite collection.", RiskOrganize, "favourite.add_request"},
	{"remove_from_favourite", "Remove requests from a favourite collection.", RiskOrganize, "favourite.remove_requests"},
	{"reorder_favourite_requests", "Set the custom order of the requests inside a favourite collection.", RiskOrganize, "favourite.reorder"},
	{"set_favourite_request_colors", "Set or clear the colour of requests as shown inside a favourite collection.", RiskOrganize, "favourite.set_color"},

	// openapi import (export is a file write and is never exposed)
	{"import_openapi", "Import an OpenAPI 3.x spec from a local file path into a project. Creates one collection per tag; requests the collection already has are left alone, new ones are added. Not undoable: delete the created collections to revert.", RiskOrganize, "openapi.import"},
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
