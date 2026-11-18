package openapi

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"snap-rq/backend/commands"
)

// preview.go — what an import would create, without writing anything. The
// import dialog uses it to let the user tick which collections to bring in.

type Preview struct{ tx *sql.Tx }

func NewPreview(tx *sql.Tx) commands.Command { return &Preview{tx: tx} }

func (Preview) Info() commands.Info {
	return commands.Info{FunctionID: "openapi.preview", Label: "Preview OpenAPI Import", ReadOnly: true}
}

type previewCollection struct {
	Name         string `json:"name"`
	RequestCount int    `json:"request_count"`
	// Exists is true when the project already has a collection with this
	// name. Such a collection is skipped on import, never changed.
	Exists bool `json:"exists"`
}

func (c Preview) Run(input json.RawMessage) (any, error) {
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

	plan, err := loadPlan(in.FilePath)
	if err != nil {
		return nil, err
	}
	existing, err := existingCollectionNames(c.tx, in.ProjectID)
	if err != nil {
		return nil, err
	}

	out := make([]previewCollection, 0, len(plan.Collections))
	for _, col := range plan.Collections {
		out = append(out, previewCollection{
			Name:         col.Name,
			RequestCount: len(col.Requests),
			Exists:       existing[strings.ToLower(col.Name)],
		})
	}
	return map[string]any{"collections": out}, nil
}
