package openapi

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

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
		// Collections lists the spec collections to import by name. Left out
		// (as the MCP tool does), every collection is imported.
		Collections *[]string `json:"collections"`
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

	var wanted map[string]bool
	if in.Collections != nil {
		wanted = map[string]bool{}
		for _, name := range *in.Collections {
			wanted[strings.ToLower(name)] = true
		}
	}

	existing, err := existingCollectionNames(c.tx, in.ProjectID)
	if err != nil {
		return nil, err
	}

	var collectionsCreated, requestsCreated, collectionsSkipped int
	for _, col := range plan.Collections {
		key := strings.ToLower(col.Name)
		if wanted != nil && !wanted[key] {
			continue
		}
		// An existing collection is never changed, and a name is only used once.
		if existing[key] {
			collectionsSkipped++
			continue
		}
		existing[key] = true

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
		"collections_skipped": collectionsSkipped,
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

// loadPlan reads a spec file and flattens it into collections and requests.
// External refs stay off: a spec must not make the app read other files or
// URLs on its own.
func loadPlan(filePath string) (importPlan, error) {
	loader := openapi3.Loader{IsExternalRefsAllowed: false}
	doc, err := loader.LoadFromFile(filePath)
	if err != nil {
		return importPlan{}, fmt.Errorf("parsing openapi: %w", err)
	}
	if doc.Paths == nil || doc.Paths.Len() == 0 {
		return importPlan{}, fmt.Errorf("this spec has no paths, nothing to import")
	}
	return buildPlan(doc), nil
}

// existingCollectionNames returns the project's collection names, lower-cased,
// because names are unique without regard to case.
func existingCollectionNames(tx *sql.Tx, projectID int64) (map[string]bool, error) {
	rows, err := tx.Query(`SELECT name FROM collections WHERE project_id = ?`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names[strings.ToLower(name)] = true
	}
	return names, rows.Err()
}
