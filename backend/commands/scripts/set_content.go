package scripts

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"snap-rq/backend/commands"
)

// set_content.go — save a script's text. Deliberately not undoable, like
// request.update: editing is continuous typing and the editor has its own
// undo.

type SetContent struct{ tx *sql.Tx }

func NewSetContent(tx *sql.Tx) commands.Command { return &SetContent{tx: tx} }

func (SetContent) Info() commands.Info {
	return commands.Info{FunctionID: "script.set_content", Label: "Edit Script"}
}

func (c SetContent) Run(input json.RawMessage) (any, error) {
	var in struct {
		ID      int64  `json:"id"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if in.ID == 0 {
		return nil, errors.New("script id is required")
	}
	result, err := c.tx.Exec(`UPDATE scripts SET content = ? WHERE id = ?`, in.Content, in.ID)
	if err != nil {
		return nil, fmt.Errorf("saving script: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return nil, fmt.Errorf("script %d not found", in.ID)
	}
	return nil, nil
}
