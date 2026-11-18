package openapi

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// requestSeed is a request flattened into the columns SnapRQ stores.
type requestSeed struct {
	Name     string
	URL      string
	Method   string
	Body     string
	BodyType string
	BodyForm string
	Headers  string
}

type collectionSeed struct {
	Name     string
	Requests []requestSeed
}

type importPlan struct {
	Collections []collectionSeed
}

// buildPlan flattens an OpenAPI document into collections (one per tag, or the
// spec title for untagged operations) and the requests each holds.
func buildPlan(doc *openapi3.T) importPlan {
	title := defaultTitle(doc)
	base := firstServerURL(doc)

	byTag := map[string]*collectionSeed{}
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			tag := title
			if len(op.Tags) > 0 {
				tag = op.Tags[0]
			}
			col, ok := byTag[tag]
			if !ok {
				col = &collectionSeed{Name: tag}
				byTag[tag] = col
			}
			col.Requests = append(col.Requests, operationSeed(base, path, method, op))
		}
	}

	names := make([]string, 0, len(byTag))
	for name := range byTag {
		names = append(names, name)
	}
	sort.Strings(names)

	plan := importPlan{}
	for _, name := range names {
		col := byTag[name]
		sort.Slice(col.Requests, func(i, j int) bool {
			if col.Requests[i].URL == col.Requests[j].URL {
				return col.Requests[i].Method < col.Requests[j].Method
			}
			return col.Requests[i].URL < col.Requests[j].URL
		})
		plan.Collections = append(plan.Collections, *col)
	}
	return plan
}

func defaultTitle(doc *openapi3.T) string {
	if doc.Info != nil && strings.TrimSpace(doc.Info.Title) != "" {
		return strings.TrimSpace(doc.Info.Title)
	}
	return "Default"
}

func firstServerURL(doc *openapi3.T) string {
	if len(doc.Servers) > 0 && doc.Servers[0] != nil {
		return strings.TrimRight(doc.Servers[0].URL, "/")
	}
	return ""
}

func operationSeed(base, path, method string, op *openapi3.Operation) requestSeed {
	seed := requestSeed{
		Name:   operationName(op, method, path),
		Method: strings.ToUpper(method),
		URL:    buildURL(base, path),
	}

	var headers []string
	var query []string
	for _, p := range op.Parameters {
		if p == nil || p.Value == nil {
			continue
		}
		v := p.Value
		switch v.In {
		case "header":
			headers = append(headers, fmt.Sprintf("%s: {{%s}}", v.Name, v.Name))
		case "query":
			query = append(query, fmt.Sprintf("%s={{%s}}", v.Name, v.Name))
		case "cookie":
			headers = append(headers, fmt.Sprintf("Cookie: %s={{%s}}", v.Name, v.Name))
		}
	}
	seed.URL = appendQuery(seed.URL, query)
	seed.Headers = strings.Join(headers, "\n")

	applyRequestBody(&seed, op)
	return seed
}

func operationName(op *openapi3.Operation, method, path string) string {
	if id := strings.TrimSpace(op.OperationID); id != "" {
		return id
	}
	return fmt.Sprintf("%s %s", strings.ToUpper(method), path)
}

// buildURL keeps OpenAPI {param} placeholders readable as SnapRQ {{param}}.
func buildURL(base, path string) string {
	expanded := strings.ReplaceAll(strings.ReplaceAll(path, "{", "{{"), "}", "}}")
	return base + expanded
}

func appendQuery(u string, query []string) string {
	if len(query) == 0 {
		return u
	}
	sep := "?"
	if strings.Contains(u, "?") {
		sep = "&"
	}
	return u + sep + strings.Join(query, "&")
}

func applyRequestBody(seed *requestSeed, op *openapi3.Operation) {
	if op.RequestBody == nil || op.RequestBody.Value == nil {
		return
	}
	content := op.RequestBody.Value.Content

	// Preferred, deterministic order of media types.
	for _, media := range []string{
		"application/json",
		"application/x-www-form-urlencoded",
		"multipart/form-data",
		"text/plain",
		"application/xml",
		"text/xml",
		"application/octet-stream",
	} {
		mt, ok := content[media]
		if !ok {
			continue
		}
		switch media {
		case "application/json":
			seed.Body = mediaExample(mt, "{}")
			seed.BodyType = "json"
		case "multipart/form-data":
			seed.BodyForm = formRows(mt)
			seed.BodyType = "form-data"
		default:
			seed.Body = mediaExample(mt, "")
			seed.BodyType = "text"
		}
		return
	}
}

func mediaExample(mt *openapi3.MediaType, fallback string) string {
	if mt.Example != nil {
		return asString(mt.Example)
	}
	return fallback
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.RawMessage:
		return string(t)
	default:
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(b)
	}
}

// formRow matches the body_form JSON the frontend and curl render already use.
type formRow struct {
	Key      string `json:"key"`
	Type     string `json:"type"`
	Value    string `json:"value"`
	FilePath string `json:"file_path"`
	FileName string `json:"file_name"`
	Enabled  bool   `json:"enabled"`
}

func formRows(mt *openapi3.MediaType) string {
	var rows []formRow
	if mt.Schema != nil && mt.Schema.Value != nil && mt.Schema.Value.Properties != nil {
		keys := make([]string, 0, len(mt.Schema.Value.Properties))
		for key := range mt.Schema.Value.Properties {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			rows = append(rows, formRow{Key: key, Type: "text", Enabled: true})
		}
	}
	b, err := json.Marshal(rows)
	if err != nil {
		return "[]"
	}
	return string(b)
}
