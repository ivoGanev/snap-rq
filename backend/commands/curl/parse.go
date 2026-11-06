// Package curl holds the curl helper commands. They are pure functions:
// nothing is persisted, so they run lock-free like every other read.
package curl

import (
	"database/sql"
	"encoding/json"

	"snap-rq/backend/commands"
)

// parse.go — turn a curl command into a request definition, attached to
// the given collection. Nothing is persisted.

type Parse struct{ tx *sql.Tx }

func NewParse(tx *sql.Tx) commands.Command { return &Parse{tx: tx} }

func (Parse) Info() commands.Info {
	return commands.Info{FunctionID: "curl.parse", Label: "Parse curl", ReadOnly: true}
}

func (c Parse) Run(input json.RawMessage) (any, error) {
	var in struct {
		CollectionID int64  `json:"collection_id"`
		Command      string `json:"command"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	return CurlToRequest(in.CollectionID, in.Command)
}
