package mcp

import (
	"errors"
	"fmt"
	"strings"
)

// Typed inputs for the organize tools. Where a tool forwards to one command
// the JSON names are that command's input names. Colours are slots, not hex:
// primary, secondary, tertiary, quaternary, quinary, senary.

type createCollectionInput struct {
	ProjectID int64  `json:"project_id" jsonschema:"id of the project"`
	Name      string `json:"name" jsonschema:"name of the new collection"`
}

type renameInput struct {
	ID   int64  `json:"id" jsonschema:"id of the collection or favourite to rename"`
	Name string `json:"name" jsonschema:"the new name"`
}

type createRequestInput struct {
	CollectionID int64  `json:"collection_id" jsonschema:"collection the request is added to"`
	Name         string `json:"name" jsonschema:"request name"`
	Method       string `json:"method,omitempty" jsonschema:"HTTP method, default GET"`
}

type deleteRequestsInput struct {
	IDs []int64 `json:"ids" jsonschema:"ids of the requests to move to the trash"`
}

type restoreInput struct {
	Items []restoreItem `json:"items" jsonschema:"trash entries to restore"`
}

type restoreItem struct {
	BinID        int64 `json:"bin_id" jsonschema:"id of the trash entry (from list_trash)"`
	CollectionID int64 `json:"collection_id" jsonschema:"collection to restore the request into"`
}

type bulkEditInput struct {
	IDs   []int64 `json:"ids" jsonschema:"ids of the requests to change"`
	Field string  `json:"field" jsonschema:"name or color"`
	Value any     `json:"value" jsonschema:"the new value; for color use a slot name, or null to clear"`
}

type reorderRequestsInput struct {
	CollectionID int64   `json:"collection_id" jsonschema:"id of the collection"`
	OrderedIDs   []int64 `json:"ordered_ids" jsonschema:"all request ids of the collection in the wanted order"`
}

type moveRequestsInput struct {
	IDs          []int64 `json:"ids" jsonschema:"ids of the requests to move"`
	CollectionID int64   `json:"collection_id" jsonschema:"collection to move them to"`
}

type attachScriptInput struct {
	RequestID  int64  `json:"request_id" jsonschema:"id of the request"`
	ProjectID  int64  `json:"project_id" jsonschema:"id of the request's project"`
	ScriptName string `json:"script_name" jsonschema:"script name (created if new)"`
}

type detachScriptInput struct {
	RequestID  int64  `json:"request_id" jsonschema:"id of the request"`
	ScriptName string `json:"script_name" jsonschema:"script name"`
}

type renameScriptInput struct {
	ProjectID int64  `json:"project_id" jsonschema:"id of the project"`
	OldName   string `json:"old_name" jsonschema:"current script name"`
	NewName   string `json:"new_name" jsonschema:"new script name"`
}

type reorderScriptInput struct {
	ProjectID  int64   `json:"project_id" jsonschema:"id of the project"`
	ScriptName string  `json:"script_name" jsonschema:"script name"`
	OrderedIDs []int64 `json:"ordered_request_ids" jsonschema:"the attached request ids in the wanted order"`
}

type scriptColorInput struct {
	ProjectID  int64   `json:"project_id" jsonschema:"id of the project"`
	ScriptName string  `json:"script_name" jsonschema:"script name"`
	RequestIDs []int64 `json:"request_ids" jsonschema:"requests to colour"`
	Color      *string `json:"color" jsonschema:"colour slot, or null to clear"`
}

type createFavouriteInput struct {
	ProjectID int64  `json:"project_id" jsonschema:"id of the project"`
	Name      string `json:"name" jsonschema:"name of the favourite collection"`
}

type addToFavouriteInput struct {
	FavouriteCollectionID int64 `json:"favourite_collection_id" jsonschema:"id of the favourite collection"`
	RequestID             int64 `json:"request_id" jsonschema:"id of the request"`
}

type removeFromFavouriteInput struct {
	FavouriteCollectionID int64   `json:"favourite_collection_id" jsonschema:"id of the favourite collection"`
	RequestIDs            []int64 `json:"request_ids" jsonschema:"requests to remove"`
}

type reorderFavouriteInput struct {
	FavouriteCollectionID int64   `json:"favourite_collection_id" jsonschema:"id of the favourite collection"`
	OrderedRequestIDs     []int64 `json:"ordered_request_ids" jsonschema:"the request ids in the wanted order"`
}

type favouriteColorInput struct {
	FavouriteCollectionID int64   `json:"favourite_collection_id" jsonschema:"id of the favourite collection"`
	RequestIDs            []int64 `json:"request_ids" jsonschema:"requests to colour"`
	Color                 *string `json:"color" jsonschema:"colour slot, or null to clear"`
}

type importOpenApiInput struct {
	ProjectID int64  `json:"project_id" jsonschema:"id of the project to import into"`
	FilePath  string `json:"file_path" jsonschema:"absolute path to the OpenAPI 3.x JSON or YAML file"`
}

func init() {
	bindings = append(bindings,
		binding{"create_collection", addTool[createCollectionInput]},
		binding{"rename_collection", addTool[renameInput]},

		binding{"create_request", customTool(createRequest)},
		binding{"duplicate_request", addTool[idInput]},
		binding{"delete_requests", addTool[deleteRequestsInput]},
		binding{"restore_from_trash", addTool[restoreInput]},
		binding{"bulk_edit_requests", customTool(bulkEdit)},
		binding{"reorder_requests", addTool[reorderRequestsInput]},
		binding{"move_requests", customTool(moveRequests)},

		binding{"attach_script", addTool[attachScriptInput]},
		binding{"detach_script", addTool[detachScriptInput]},
		binding{"rename_script", addTool[renameScriptInput]},
		binding{"reorder_script_requests", addTool[reorderScriptInput]},
		binding{"set_script_request_colors", addTool[scriptColorInput]},

		binding{"create_favourite", addTool[createFavouriteInput]},
		binding{"rename_favourite", addTool[renameInput]},
		binding{"add_to_favourite", addTool[addToFavouriteInput]},
		binding{"remove_from_favourite", addTool[removeFromFavouriteInput]},
		binding{"reorder_favourite_requests", addTool[reorderFavouriteInput]},
		binding{"set_favourite_request_colors", addTool[favouriteColorInput]},

		binding{"import_openapi", addTool[importOpenApiInput]},
	)
}

// bulkEdit changes only the organizing fields. What a request sends is
// edit_request_content's job, a separate tool that is off by default.
func bulkEdit(s *Server, in bulkEditInput) (any, error) {
	if in.Field != "name" && in.Field != "color" {
		return nil, fmt.Errorf("bulk_edit_requests only changes name and color, not %q; changing what a request sends is edit_request_content, which the user must switch on in the MCP settings", in.Field)
	}
	return s.run("request.bulk_set", in)
}

// createRequest fills in the method before creating, so a request is never
// stored without one.
func createRequest(s *Server, in createRequestInput) (any, error) {
	if in.CollectionID == 0 {
		return nil, errors.New("collection_id is required")
	}
	in.Method = strings.ToUpper(strings.TrimSpace(in.Method))
	if in.Method == "" {
		in.Method = "GET"
	}
	return s.run("request.create", in)
}

// moveRequests moves requests by reading each one and writing it back with a
// new collection: request.update takes a whole request and appends it to the
// target collection's order. The UI moves requests the same way, so a move is
// not undoable. On failure the error says which requests were already moved.
func moveRequests(s *Server, in moveRequestsInput) (any, error) {
	if len(in.IDs) == 0 {
		return nil, errors.New("ids must not be empty")
	}
	if in.CollectionID == 0 {
		return nil, errors.New("collection_id is required")
	}
	moved := []int64{}
	for _, id := range in.IDs {
		req, err := s.runMap("request.get", map[string]any{"id": id})
		if err != nil {
			return nil, moveFailure(moved, id, err)
		}
		req["collection_id"] = in.CollectionID
		if _, err := s.run("request.update", req); err != nil {
			return nil, moveFailure(moved, id, err)
		}
		moved = append(moved, id)
	}
	return map[string]any{"moved": moved, "collection_id": in.CollectionID}, nil
}

func moveFailure(moved []int64, failedID int64, err error) error {
	return fmt.Errorf("request %d was not moved (already moved: %v): %w", failedID, moved, err)
}
