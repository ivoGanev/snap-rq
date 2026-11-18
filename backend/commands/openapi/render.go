package openapi

import (
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

type exportCounts struct {
	CollectionsExported int `json:"collections_exported"`
	RequestsExported    int `json:"requests_exported"`
	// RequestsSkipped counts requests left out because another request
	// already uses the same method and path, or the method is not one
	// OpenAPI can describe.
	RequestsSkipped int `json:"requests_skipped"`
}

var exportMethods = map[string]bool{
	"GET": true, "PUT": true, "POST": true, "DELETE": true,
	"OPTIONS": true, "HEAD": true, "PATCH": true, "TRACE": true,
}

// variablePattern matches a SnapRQ {{variable}}.
var variablePattern = regexp.MustCompile(`\{\{\s*([^{}\s]+)\s*\}\}`)

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
	usedIDs := map[string]bool{}
	counts := exportCounts{CollectionsExported: len(collections)}

	for _, col := range collections {
		doc.Tags = append(doc.Tags, &openapi3.Tag{Name: col.Name})
		for _, req := range col.Requests {
			method := strings.ToUpper(strings.TrimSpace(req.Method))
			path := specPath(pathOf(req.URL))
			if !exportMethods[method] {
				counts.RequestsSkipped++
				continue
			}
			item := doc.Paths.Value(path)
			if item != nil && item.GetOperation(method) != nil {
				counts.RequestsSkipped++
				continue
			}
			if item == nil {
				item = &openapi3.PathItem{}
				doc.Paths.Set(path, item)
			}

			counts.RequestsExported++
			if origin := originOf(req.URL); origin != "" {
				servers[origin] = true
			}
			item.SetOperation(method, operationFor(col.Name, req, path, uniqueID(sanitizeID(req.Name), usedIDs)))
		}
	}

	for origin := range servers {
		server := &openapi3.Server{URL: origin}
		// A {{variable}} base URL becomes a server variable, so re-importing
		// the file brings the variable back.
		if strings.HasPrefix(origin, "{") && strings.HasSuffix(origin, "}") {
			name := strings.Trim(origin, "{}")
			server.Variables = map[string]*openapi3.ServerVariable{name: {Default: ""}}
		}
		doc.Servers = append(doc.Servers, server)
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

func operationFor(collection string, req exportRequest, path, id string) *openapi3.Operation {
	op := &openapi3.Operation{
		Tags:      []string{collection},
		Responses: defaultResponses(),
	}
	if id != "" {
		op.OperationID = id
	}
	var params openapi3.Parameters
	params = append(params, pathParameters(path)...)
	params = append(params, queryParameters(req.URL)...)
	params = append(params, headerParameters(req.Headers)...)
	if len(params) > 0 {
		op.Parameters = params
	}
	if rb := exportRequestBody(req); rb != nil {
		op.RequestBody = rb
	}
	return op
}

// defaultResponses is a placeholder: SnapRQ does not record what a server is
// expected to answer, but OpenAPI requires at least one response.
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
	case "form-data":
		schema := formSchema(req.BodyForm)
		if schema == nil {
			return nil
		}
		return &openapi3.RequestBodyRef{Value: &openapi3.RequestBody{
			Content: openapi3.Content{"multipart/form-data": &openapi3.MediaType{Schema: schema.NewRef()}},
		}}
	default:
		return nil
	}
}

// formSchema turns the enabled rows of a form body into an object schema.
func formSchema(raw string) *openapi3.Schema {
	var rows []formRow
	if json.Unmarshal([]byte(raw), &rows) != nil {
		return nil
	}
	schema := openapi3.NewObjectSchema()
	count := 0
	for _, row := range rows {
		if !row.Enabled || strings.TrimSpace(row.Key) == "" {
			continue
		}
		field := openapi3.NewStringSchema()
		if row.Type == "file" {
			field = field.WithFormat("binary")
		}
		schema = schema.WithProperty(row.Key, field)
		count++
	}
	if count == 0 {
		return nil
	}
	return schema
}

// specPath writes SnapRQ {{variable}} placeholders the way OpenAPI does.
func specPath(path string) string {
	return variablePattern.ReplaceAllString(path, "{$1}")
}

func pathParameters(path string) openapi3.Parameters {
	var params openapi3.Parameters
	seen := map[string]bool{}
	for _, m := range regexp.MustCompile(`\{([^{}/]+)\}`).FindAllStringSubmatch(path, -1) {
		if seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		params = append(params, &openapi3.ParameterRef{Value: &openapi3.Parameter{
			Name:     m[1],
			In:       "path",
			Required: true,
			Schema:   &openapi3.SchemaRef{Value: openapi3.NewStringSchema()},
		}})
	}
	return params
}

// ignoredHeaders are headers OpenAPI says must not be described as parameters.
var ignoredHeaders = map[string]bool{"accept": true, "content-type": true, "authorization": true, "cookie": true}

// headerParameters reads the "Name: value" lines SnapRQ stores. Values are
// left out on purpose: they often hold tokens.
func headerParameters(raw string) openapi3.Parameters {
	var params openapi3.Parameters
	seen := map[string]bool{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		name, _, found := strings.Cut(line, ":")
		name = strings.TrimSpace(name)
		if !found || name == "" || ignoredHeaders[strings.ToLower(name)] || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		params = append(params, &openapi3.ParameterRef{Value: &openapi3.Parameter{
			Name:   name,
			In:     "header",
			Schema: &openapi3.SchemaRef{Value: openapi3.NewStringSchema()},
		}})
	}
	return params
}

// uniqueID returns id, or id_2, id_3... when it was already used. Empty stays empty.
func uniqueID(id string, used map[string]bool) string {
	if id == "" {
		return ""
	}
	candidate := id
	for n := 2; used[candidate]; n++ {
		candidate = id + "_" + strconv.Itoa(n)
	}
	used[candidate] = true
	return candidate
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

// leadingVariable splits a URL that starts with a {{variable}} base, such as
// {{base_url}}/pets, into the variable name and the rest.
func leadingVariable(s string) (name, rest string, ok bool) {
	m := variablePattern.FindStringSubmatchIndex(s)
	if m == nil || m[0] != 0 {
		return "", s, false
	}
	return s[m[2]:m[3]], s[m[1]:], true
}

func originOf(raw string) string {
	s := strings.TrimSpace(raw)
	if name, _, ok := leadingVariable(s); ok {
		return "{" + name + "}"
	}
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
	if _, after, ok := leadingVariable(s); ok {
		s = after
		if s == "" {
			return "/"
		}
	}
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
