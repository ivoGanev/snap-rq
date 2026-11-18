package openapi

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

type exportCounts struct {
	CollectionsExported int `json:"collections_exported"`
	RequestsExported    int `json:"requests_exported"`
}

type exportRequest struct {
	Name, URL, Method, Body, BodyType, BodyForm, Headers string
}

type exportCollection struct {
	Name     string
	Requests []exportRequest
}

// buildDocument reads the project's collections and requests and shapes them
// into one OpenAPI 3.0 document. Each SnapRQ collection becomes a tag, and
// each request an operation tagged with its collection.
func (c Export) buildDocument(projectID int64) (*openapi3.T, exportCounts, error) {
	title, err := c.projectName(projectID)
	if err != nil {
		return nil, exportCounts{}, err
	}
	if title == "" {
		title = "SnapRQ export"
	}

	collections, err := c.loadCollections(projectID)
	if err != nil {
		return nil, exportCounts{}, err
	}

	doc := &openapi3.T{
		OpenAPI: "3.0.3",
		Info:    &openapi3.Info{Title: title, Version: "1.0.0"},
		Paths:   openapi3.NewPaths(),
	}

	servers := map[string]bool{}
	counts := exportCounts{CollectionsExported: len(collections)}

	for _, col := range collections {
		doc.Tags = append(doc.Tags, &openapi3.Tag{Name: col.Name})
		for _, req := range col.Requests {
			counts.RequestsExported++
			if origin := originOf(req.URL); origin != "" {
				servers[origin] = true
			}

			path := pathOf(req.URL)
			item := doc.Paths.Value(path)
			if item == nil {
				item = &openapi3.PathItem{}
				doc.Paths.Set(path, item)
			}
			item.SetOperation(strings.ToUpper(strings.TrimSpace(req.Method)), operationFor(col.Name, req))
		}
	}

	for origin := range servers {
		doc.Servers = append(doc.Servers, &openapi3.Server{URL: origin})
	}
	sort.Slice(doc.Servers, func(i, j int) bool { return doc.Servers[i].URL < doc.Servers[j].URL })

	return doc, counts, nil
}

func (c Export) projectName(projectID int64) (string, error) {
	var name string
	err := c.tx.QueryRow(`SELECT name FROM projects WHERE id = ?`, projectID).Scan(&name)
	return name, err
}

func (c Export) loadCollections(projectID int64) ([]exportCollection, error) {
	rows, err := c.tx.Query(`SELECT id, name FROM collections WHERE project_id = ? ORDER BY id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []exportCollection
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		reqs, err := c.loadRequests(id)
		if err != nil {
			return nil, err
		}
		out = append(out, exportCollection{Name: name, Requests: reqs})
	}
	return out, rows.Err()
}

func (c Export) loadRequests(collectionID int64) ([]exportRequest, error) {
	rows, err := c.tx.Query(
		`SELECT name, url, method, COALESCE(body, ''), COALESCE(body_type, 'json'),
		        COALESCE(body_form, ''), COALESCE(request_headers, '')
		   FROM http_requests
		  WHERE collection_id = ?
		  ORDER BY user_sort_order, id`, collectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []exportRequest
	for rows.Next() {
		var r exportRequest
		if err := rows.Scan(&r.Name, &r.URL, &r.Method, &r.Body, &r.BodyType, &r.BodyForm, &r.Headers); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func operationFor(collection string, req exportRequest) *openapi3.Operation {
	op := &openapi3.Operation{
		Tags:      []string{collection},
		Responses: defaultResponses(),
	}
	if id := sanitizeID(req.Name); id != "" {
		op.OperationID = id
	}
	if params := queryParameters(req.URL); len(params) > 0 {
		op.Parameters = params
	}
	if rb := exportRequestBody(req); rb != nil {
		op.RequestBody = rb
	}
	return op
}

func defaultResponses() *openapi3.Responses {
	responses := openapi3.NewResponses()
	responses.Set("200", &openapi3.ResponseRef{Value: openapi3.NewResponse().WithDescription("OK")})
	return responses
}

func exportRequestBody(req exportRequest) *openapi3.RequestBodyRef {
	switch req.BodyType {
	case "json":
		if strings.TrimSpace(req.Body) == "" {
			return nil
		}
		var example any
		if json.Unmarshal([]byte(req.Body), &example) != nil {
			example = req.Body
		}
		return &openapi3.RequestBodyRef{Value: &openapi3.RequestBody{
			Content: openapi3.Content{"application/json": &openapi3.MediaType{Example: example}},
		}}
	case "text":
		if strings.TrimSpace(req.Body) == "" {
			return nil
		}
		return &openapi3.RequestBodyRef{Value: &openapi3.RequestBody{
			Content: openapi3.Content{"text/plain": &openapi3.MediaType{Example: req.Body}},
		}}
	default:
		return nil
	}
}

func queryParameters(raw string) openapi3.Parameters {
	i := strings.Index(raw, "?")
	if i < 0 {
		return nil
	}
	query := raw[i+1:]
	if j := strings.IndexAny(query, "#"); j >= 0 {
		query = query[:j]
	}

	var params openapi3.Parameters
	for _, pair := range strings.Split(query, "&") {
		if pair == "" {
			continue
		}
		name, _, found := strings.Cut(pair, "=")
		if !found || name == "" {
			continue
		}
		params = append(params, &openapi3.ParameterRef{
			Value: &openapi3.Parameter{
				Name:   name,
				In:     "query",
				Schema: &openapi3.SchemaRef{Value: openapi3.NewStringSchema()},
			},
		})
	}
	return params
}

func originOf(raw string) string {
	s := strings.TrimSpace(raw)
	i := strings.Index(s, "://")
	if i < 0 {
		return ""
	}
	rest := s[i+3:]
	if j := strings.IndexAny(rest, "/?#"); j >= 0 {
		return s[:i+3] + rest[:j]
	}
	return s
}

func pathOf(raw string) string {
	s := strings.TrimSpace(raw)
	rest := s
	if i := strings.Index(s, "://"); i >= 0 {
		rest = s[i+3:]
	}
	j := strings.IndexAny(rest, "/?#")
	if j < 0 {
		return "/"
	}
	rest = rest[j:]
	if strings.HasPrefix(rest, "?") || strings.HasPrefix(rest, "#") {
		return "/"
	}
	if k := strings.IndexAny(rest, "?#"); k >= 0 {
		rest = rest[:k]
	}
	return rest
}

func sanitizeID(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}
