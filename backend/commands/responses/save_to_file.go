package responses

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"

	"snap-rq/backend/commands"
)

// save_to_file.go — write a response's body to a file. It changes nothing
// in the database (runs lock-free); the file write is an external effect,
// never undone.

type SaveToFile struct{ tx *sql.Tx }

func NewSaveToFile(tx *sql.Tx) commands.Command { return &SaveToFile{tx: tx} }

func (SaveToFile) Info() commands.Info {
	return commands.Info{FunctionID: "response.save_to_file", Label: "Save Response To File", ReadOnly: true}
}

func (c SaveToFile) Run(input json.RawMessage) (any, error) {
	var in struct {
		ResponseID int64  `json:"response_id"`
		FilePath   string `json:"file_path"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	var body string
	if err := c.tx.QueryRow(
		`SELECT COALESCE(body, '') FROM responses WHERE id = ?`, in.ResponseID,
	).Scan(&body); err != nil {
		return nil, fmt.Errorf("loading the response: %w", err)
	}
	if err := os.WriteFile(in.FilePath, []byte(body), 0644); err != nil {
		return nil, fmt.Errorf("writing the file: %w", err)
	}
	return nil, nil
}
