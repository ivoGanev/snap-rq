package openapi

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/getkin/kin-openapi/openapi3"
	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// import.go — build collections and requests from an OpenAPI file. It is a
// bulk load of new rows, so it is a plain command, not an UndoableCommand.

type Import struct{ tx *sql.Tx }

func NewImport(tx *sql.Tx) commands.Command { return &Import{tx: tx} }

func (Import) Info() commands.Info {
	return commands.Info{FunctionID: "openapi.import", Label: "Import OpenAPI"}
}

func (c Import) Run(input json.RawMessage) (any, error) {
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

	// External refs stay off: a spec must not make the app read other files
	// or URLs on its own.
	loader := openapi3.Loader{IsExternalRefsAllowed: false}
	doc, err := loader.LoadFromFile(in.FilePath)
	if err != nil {
		return nil, fmt.Errorf("parsing openapi: %w", err)
	}

	if doc.Paths == nil || doc.Paths.Len() == 0 {
		return nil, fmt.Errorf("this spec has no paths, nothing to import")
	}

	plan := buildPlan(doc)

	var collectionsCreated, requestsCreated int
	for _, col := range plan.Collections {
		colID, err := c.insertCollection(in.ProjectID, col.Name)
		if err != nil {
			return nil, err
		}
		collectionsCreated++
		for i, req := range col.Requests {
			if err := c.insertRequest(colID, req, i); err != nil {
				return nil, err
			}
			requestsCreated++
		}
	}

	return map[string]any{
		"collections_created": collectionsCreated,
		"requests_created":    requestsCreated,
	}, nil
}

// insertCollection mirrors collections/create.go: a row plus its default
// appearance.
func (c Import) insertCollection(projectID int64, name string) (int64, error) {
	res, err := c.tx.Exec(`INSERT INTO collections (project_id, name) VALUES (?, ?)`, projectID, name)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()

	app := models.DefaultCollectionAppearance()
	if _, err := c.tx.Exec(
		`INSERT INTO collection_appearances (collection_id, appearance_type, appearance_value) VALUES (?, ?, ?)`,
		id, app.AppearanceType, app.AppearanceValue,
	); err != nil {
		return 0, err
	}
	return id, nil
}

// insertRequest mirrors requests/create.go's columns. user_sort_order is the
// seed's index within its (fresh) collection.
func (c Import) insertRequest(colID int64, req requestSeed, order int) error {
	_, err := c.tx.Exec(
		`INSERT INTO http_requests
		   (collection_id, name, url, method, body, body_type, body_form, request_headers, user_sort_order)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		colID, req.Name, req.URL, req.Method, req.Body, normalizeBodyType(req.BodyType), req.BodyForm, req.Headers, order,
	)
	return err
}

func normalizeBodyType(t string) string {
	switch t {
	case "json", "text", "form-data":
		return t
	default:
		return "json"
	}
}
