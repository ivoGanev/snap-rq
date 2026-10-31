package runnables

import (
	"encoding/json"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
	"snap-rq/backend/store"
)

// curl commands are pure functions: they change nothing in the database,
// so they run lock-free like every other ReadOnly command.

// ParseCurl turns a curl command into a request definition, attached to
// the given collection. Nothing is persisted.
type ParseCurl struct{}

func (ParseCurl) Info() commands.Info {
	return commands.Info{FunctionID: "curl.parse", Label: "Parse curl", ReadOnly: true}
}

func (ParseCurl) Run(db store.Session, input json.RawMessage) (any, error) {
	var payload struct {
		CollectionID int64  `json:"collection_id"`
		Command      string `json:"command"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, err
	}
	return CurlToRequest(payload.CollectionID, payload.Command)
}

// RenderCurl turns a request definition into an equivalent curl command.
type RenderCurl struct{}

func (RenderCurl) Info() commands.Info {
	return commands.Info{FunctionID: "curl.render", Label: "Render curl", ReadOnly: true}
}

func (RenderCurl) Run(_ store.Session, input json.RawMessage) (any, error) {
	var req models.HttpRequest
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	return RequestToCurl(req), nil
}
