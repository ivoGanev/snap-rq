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

// collectionPick is what the user ticked for one collection in the dialog.
type collectionPick struct {
	Name   string   `json:"name"`
	Add    []string `json:"add"`
	Update []string `json:"update"`
}

func (c Import) Run(input json.RawMessage) (any, error) {
	var in struct {
		ProjectID int64  `json:"project_id"`
		FilePath  string `json:"file_path"`
		// Collections is what to bring in. Left out (as the MCP tool does),
		// every request the project does not have yet is added and nothing
		// existing is changed.
		Collections *[]collectionPick `json:"collections"`
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
	// Compared again here, so a project that changed since the preview is
	// handled by what it holds now.
	diffs, err := diffPlan(c.tx, in.ProjectID, plan)
	if err != nil {
		return nil, err
	}

	picks := map[string]collectionPick{}
	if in.Collections != nil {
		for _, p := range *in.Collections {
			picks[strings.ToLower(p.Name)] = p
		}
	}

	var collectionsCreated, requestsCreated, requestsUpdated int
	created := map[string]int64{} // collections made in this run, by lower-cased name
	for _, d := range diffs {
		add, update := map[string]bool{}, map[string]bool{}
		if in.Collections == nil {
			for _, r := range d.requests {
				if r.Status == statusNew {
					add[r.Key] = true
				}
			}
		} else {
			pick, ok := picks[strings.ToLower(d.name)]
			if !ok {
				continue
			}
			for _, k := range pick.Add {
				add[k] = true
			}
			for _, k := range pick.Update {
				update[k] = true
			}
		}

		colID := d.colID
		if colID == 0 {
			colID = created[strings.ToLower(d.name)]
		}
		next := 0
		if colID != 0 {
			if err := c.tx.QueryRow(
				`SELECT COALESCE(MAX(user_sort_order), -1) + 1 FROM http_requests WHERE collection_id = ?`, colID,
			).Scan(&next); err != nil {
				return nil, err
			}
		}

		for _, r := range d.requests {
			switch {
			case r.Status == statusNew && add[r.Key]:
				if colID == 0 {
					if colID, err = c.insertCollection(in.ProjectID, d.name); err != nil {
						return nil, err
					}
					created[strings.ToLower(d.name)] = colID
					collectionsCreated++
				}
				if err := c.insertRequest(colID, r.seed, next); err != nil {
					return nil, err
				}
				next++
				requestsCreated++
			case r.Status == statusChanged && update[r.Key]:
				if err := c.updateRequest(applyChanges(*r.stored, r.seed, r.Changes)); err != nil {
					return nil, err
				}
				requestsUpdated++
			}
		}
	}

	return map[string]any{
		"collections_created": collectionsCreated,
		"requests_created":    requestsCreated,
		"requests_updated":    requestsUpdated,
	}, nil
}

// updateRequest writes back the fields a spec can change.
func (c Import) updateRequest(r storedRequest) error {
	_, err := c.tx.Exec(
		`UPDATE http_requests
		    SET url = ?, request_headers = ?, body = ?, body_type = ?, body_form = ?
		  WHERE id = ?`,
		r.URL, r.Headers, r.Body, r.BodyType, r.BodyForm, r.ID,
	)
	return err
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
