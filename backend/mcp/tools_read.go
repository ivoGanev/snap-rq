package mcp

// Typed inputs for the read tools. The JSON names match the commands' own
// input fields, so arguments pass straight through to the runner.

type noInput struct{}

type projectInput struct {
	ProjectID int64 `json:"project_id" jsonschema:"id of the project"`
}

type collectionInput struct {
	CollectionID int64 `json:"collection_id" jsonschema:"id of the collection"`
}

type recentInput struct {
	ProjectID int64 `json:"project_id" jsonschema:"id of the project"`
	Limit     int   `json:"limit" jsonschema:"how many requests to return"`
}

type idInput struct {
	ID int64 `json:"id" jsonschema:"id of the request"`
}

type requestIDInput struct {
	RequestID int64 `json:"request_id" jsonschema:"id of the request"`
}

type scriptRequestsInput struct {
	ProjectID  int64  `json:"project_id" jsonschema:"id of the project"`
	ScriptName string `json:"script_name" jsonschema:"name of the script"`
}

// bindings is every tool's typed registration; validateBindings keeps it in
// step with Catalog.
var bindings = []binding{
	{"list_profiles", addTool[noInput]},
	{"list_projects", addTool[noInput]},
	{"list_collections", addTool[projectInput]},
	{"list_requests", addTool[collectionInput]},
	{"list_recent_requests", addTool[recentInput]},
	{"get_request", addTool[idInput]},
	{"list_responses", addTool[requestIDInput]},
	{"list_scripts", addTool[projectInput]},
	{"list_script_requests", addTool[scriptRequestsInput]},
	{"list_favourites", addTool[projectInput]},
	{"list_trash", addTool[projectInput]},
}
