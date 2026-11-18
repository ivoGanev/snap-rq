package openapi

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"

	"snap-rq/backend/commands"
)

// export.go — write every collection of a project to one OpenAPI file. Like
// response.save_to_file: reads the database, writes a file, never undone.

type Export struct{ tx *sql.Tx }

func NewExport(tx *sql.Tx) commands.Command { return &Export{tx: tx} }

func (Export) Info() commands.Info {
	return commands.Info{FunctionID: "openapi.export", Label: "Export OpenAPI", ReadOnly: true}
}

func (c Export) Run(input json.RawMessage) (any, error) {
	var in struct {
		ProjectID int64  `json:"project_id"`
		FilePath  string `json:"file_path"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if in.ProjectID == 0 {
		return nil, fmt.Errorf("project_id is required")
	}

	doc, counts, err := c.buildDocument(in.ProjectID)
	if err != nil {
		return nil, err
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(in.FilePath, append(data, '\n'), 0o644); err != nil {
		return nil, fmt.Errorf("writing the file: %w", err)
	}
	return map[string]any{
		"collections_exported": counts.CollectionsExported,
		"requests_exported":    counts.RequestsExported,
	}, nil
}
