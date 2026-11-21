package scripts

import (
	"database/sql"
	"encoding/json"

	"snap-rq/backend/commands"
	"snap-rq/backend/scripting"
)

// check.go — compile a script's code with the engine that runs it, without
// running anything, and report its syntax errors with their positions. The
// script editor calls it as you type.

type Check struct{ tx *sql.Tx }

func NewCheck(tx *sql.Tx) commands.Command { return &Check{tx: tx} }

func (Check) Info() commands.Info {
	return commands.Info{FunctionID: "script.check", Label: "Check Script", ReadOnly: true}
}

func (Check) Run(input json.RawMessage) (any, error) {
	var in struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	problems := scripting.Check(in.Content)
	if problems == nil {
		problems = []scripting.Problem{}
	}
	return problems, nil
}
