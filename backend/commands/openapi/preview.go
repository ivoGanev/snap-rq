package openapi

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"snap-rq/backend/commands"
)

// preview.go — what an import would do, without writing anything. The import
// dialog uses it to show what is new, what differs and what is unchanged.

type Preview struct{ tx *sql.Tx }

func NewPreview(tx *sql.Tx) commands.Command { return &Preview{tx: tx} }

func (Preview) Info() commands.Info {
	return commands.Info{FunctionID: "openapi.preview", Label: "Preview OpenAPI Import", ReadOnly: true}
}

type previewCollection struct {
	Name string `json:"name"`
	// Exists is true when the project already has a collection with this name;
	// the import then adds to it and updates what the user ticks.
	Exists    bool          `json:"exists"`
	New       int           `json:"new"`
	Changed   int           `json:"changed"`
	Unchanged int           `json:"unchanged"`
	Requests  []requestDiff `json:"requests"`
	// NotInSpec lists requests the collection has that the file does not. They
	// are shown for information and never changed.
	NotInSpec []string `json:"not_in_spec"`
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
	diffs, err := diffPlan(c.tx, in.ProjectID, plan)
	if err != nil {
		return nil, err
	}

	out := make([]previewCollection, 0, len(diffs))
	for _, d := range diffs {
		pc := previewCollection{
			Name:      d.name,
			Exists:    d.colID != 0,
			Requests:  d.requests,
			NotInSpec: d.notInSpec,
		}
		if pc.NotInSpec == nil {
			pc.NotInSpec = []string{}
		}
		for _, r := range d.requests {
			switch r.Status {
			case statusNew:
				pc.New++
			case statusChanged:
				pc.Changed++
			default:
				pc.Unchanged++
			}
		}
		out = append(out, pc)
	}
	return map[string]any{"collections": out}, nil
}

// collectionDiff is one spec collection compared with the project.
type collectionDiff struct {
	name      string
	colID     int64 // 0 when the project has no collection with this name
	requests  []requestDiff
	notInSpec []string
}

func diffPlan(tx *sql.Tx, projectID int64, plan importPlan) ([]collectionDiff, error) {
	ids, err := collectionIDsByName(tx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]collectionDiff, 0, len(plan.Collections))
	for _, col := range plan.Collections {
		d := collectionDiff{name: col.Name, colID: ids[strings.ToLower(col.Name)]}
		var stored []storedRequest
		if d.colID != 0 {
			if stored, err = loadStored(tx, d.colID); err != nil {
				return nil, err
			}
		}
		d.requests, d.notInSpec = diffCollection(col.Requests, stored)
		out = append(out, d)
	}
	return out, nil
}

// collectionIDsByName maps lower-cased collection names to ids; names are
// unique within a project without regard to case.
func collectionIDsByName(tx *sql.Tx, projectID int64) (map[string]int64, error) {
	rows, err := tx.Query(`SELECT id, name FROM collections WHERE project_id = ?`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := map[string]int64{}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		ids[strings.ToLower(name)] = id
	}
	return ids, rows.Err()
}

func loadStored(tx *sql.Tx, collectionID int64) ([]storedRequest, error) {
	rows, err := tx.Query(
		`SELECT id, name, url, method, COALESCE(body, ''), COALESCE(body_type, 'json'),
		        COALESCE(body_form, ''), COALESCE(request_headers, '')
		   FROM http_requests WHERE collection_id = ? ORDER BY id`, collectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []storedRequest
	for rows.Next() {
		var r storedRequest
		if err := rows.Scan(&r.ID, &r.Name, &r.URL, &r.Method, &r.Body, &r.BodyType, &r.BodyForm, &r.Headers); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
