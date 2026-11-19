package openapi

import (
	"encoding/json"
	"sort"
	"strings"
)

// diff.go — compares the requests a spec describes with the requests a
// collection already holds, so the import dialog can say what would be added
// and what would change. Everything here is pure: no database, no UI.

type requestStatus string

const (
	statusNew       requestStatus = "new"
	statusChanged   requestStatus = "changed"
	statusUnchanged requestStatus = "unchanged"
)

// storedRequest is a request row already in a collection.
type storedRequest struct {
	ID       int64
	Name     string
	URL      string
	Method   string
	Body     string
	BodyType string
	BodyForm string
	Headers  string
}

// fieldChange is one difference between a stored request and the spec.
// Field is one of "query", "headers", "body type", "body".
type fieldChange struct {
	Field  string `json:"field"`
	Before string `json:"before"`
	After  string `json:"after"`
}

type requestDiff struct {
	Key     string        `json:"key"`
	Method  string        `json:"method"`
	Path    string        `json:"path"`
	Status  requestStatus `json:"status"`
	Changes []fieldChange `json:"changes"`

	seed   requestSeed
	stored *storedRequest
}

// matchKey says whether two requests are "the same one": method plus path,
// with the server and query string ignored and {{id}} read as {id}.
func matchKey(method, url string) string {
	return strings.ToUpper(strings.TrimSpace(method)) + " " + normalisedPath(url)
}

func normalisedPath(url string) string {
	path := specPath(pathOf(url))
	if len(path) > 1 {
		path = strings.TrimRight(path, "/")
	}
	return path
}

// diffCollection compares the spec's requests with a collection's rows. The
// second result lists keys of stored requests the spec no longer has; they are
// reported but never touched.
func diffCollection(seeds []requestSeed, stored []storedRequest) ([]requestDiff, []string) {
	byKey := map[string]*storedRequest{}
	var order []string
	for i := range stored {
		key := matchKey(stored[i].Method, stored[i].URL)
		if _, taken := byKey[key]; !taken {
			byKey[key] = &stored[i]
			order = append(order, key)
		}
	}

	seen := map[string]bool{}
	diffs := make([]requestDiff, 0, len(seeds))
	for _, seed := range seeds {
		key := matchKey(seed.Method, seed.URL)
		if seen[key] {
			continue
		}
		seen[key] = true

		d := requestDiff{
			Key:     key,
			Method:  strings.ToUpper(seed.Method),
			Path:    normalisedPath(seed.URL),
			Status:  statusNew,
			Changes: []fieldChange{},
			seed:    seed,
		}
		if existing, ok := byKey[key]; ok {
			d.stored = existing
			d.Changes = compareFields(seed, *existing)
			d.Status = statusUnchanged
			if len(d.Changes) > 0 {
				d.Status = statusChanged
			}
		}
		diffs = append(diffs, d)
	}

	var gone []string
	for _, key := range order {
		if !seen[key] {
			gone = append(gone, key)
		}
	}
	return diffs, gone
}

// compareFields looks only at what a spec defines: query parameter names,
// header names, body type and body. Names, hosts and header values are left
// alone so personal edits there do not show up as changes.
func compareFields(seed requestSeed, stored storedRequest) []fieldChange {
	changes := []fieldChange{}

	// Query parameters and headers only ever count as changed when the spec
	// has one the request lacks. Extra ones on the request (an auth header,
	// say) are the user's own and are left alone.
	if have := queryNames(stored.URL); len(missingNames(queryNames(seed.URL), have)) > 0 {
		after := uniqueSorted(append(append([]string{}, have...), missingNames(queryNames(seed.URL), have)...))
		changes = append(changes, fieldChange{"query", strings.Join(have, ", "), strings.Join(after, ", ")})
	}
	if have := headerNames(stored.Headers); len(missingNames(headerNames(seed.Headers), have)) > 0 {
		after := uniqueSorted(append(append([]string{}, have...), missingNames(headerNames(seed.Headers), have)...))
		changes = append(changes, fieldChange{"headers", strings.Join(have, ", "), strings.Join(after, ", ")})
	}

	seedType := normalizeBodyType(seed.BodyType)
	storedType := normalizeBodyType(stored.BodyType)
	if seedType != storedType {
		changes = append(changes, fieldChange{"body type", storedType, seedType})
	}

	// A body only counts when both sides agree on its type.
	switch seedType {
	case "form-data":
		if storedType == seedType {
			if a, b := formKeys(stored.BodyForm), formKeys(seed.BodyForm); !sameList(a, b) {
				changes = append(changes, fieldChange{"body", strings.Join(a, ", "), strings.Join(b, ", ")})
			}
		}
	default:
		if storedType == seedType && compactBody(stored.Body) != compactBody(seed.Body) {
			changes = append(changes, fieldChange{"body", strings.TrimSpace(stored.Body), strings.TrimSpace(seed.Body)})
		}
	}
	return changes
}

func hasField(changes []fieldChange, field string) bool {
	for _, c := range changes {
		if c.Field == field {
			return true
		}
	}
	return false
}

// applyChanges returns the stored request with the spec's side of each
// changed field written in. Anything not listed in changes is kept as is.
func applyChanges(stored storedRequest, seed requestSeed, changes []fieldChange) storedRequest {
	out := stored

	if hasField(changes, "query") {
		have := map[string]bool{}
		for _, name := range queryNames(stored.URL) {
			have[strings.ToLower(name)] = true
		}
		var add []string
		if i := strings.Index(seed.URL, "?"); i >= 0 {
			for _, pair := range strings.Split(seed.URL[i+1:], "&") {
				name, _, _ := strings.Cut(pair, "=")
				if name = strings.TrimSpace(name); name != "" && !have[strings.ToLower(name)] {
					have[strings.ToLower(name)] = true
					add = append(add, pair)
				}
			}
		}
		if len(add) > 0 {
			sep := "?"
			if strings.Contains(stored.URL, "?") {
				sep = "&"
			}
			out.URL = stored.URL + sep + strings.Join(add, "&")
		}
	}

	if hasField(changes, "headers") {
		have := map[string]bool{}
		for _, name := range headerNames(stored.Headers) {
			have[strings.ToLower(name)] = true
		}
		lines := []string{}
		if strings.TrimSpace(stored.Headers) != "" {
			lines = append(lines, strings.TrimRight(stored.Headers, "\n"))
		}
		for _, line := range strings.Split(seed.Headers, "\n") {
			if name := strings.ToLower(headerName(line)); name != "" && !have[name] {
				have[name] = true
				lines = append(lines, line)
			}
		}
		out.Headers = strings.Join(lines, "\n")
	}

	if hasField(changes, "body type") || hasField(changes, "body") {
		out.BodyType = normalizeBodyType(seed.BodyType)
		out.Body = seed.Body
		out.BodyForm = seed.BodyForm
	}
	return out
}

func queryNames(url string) []string {
	i := strings.Index(url, "?")
	if i < 0 {
		return nil
	}
	query := url[i+1:]
	if j := strings.Index(query, "#"); j >= 0 {
		query = query[:j]
	}
	var names []string
	for _, pair := range strings.Split(query, "&") {
		if name, _, _ := strings.Cut(pair, "="); strings.TrimSpace(name) != "" {
			names = append(names, strings.TrimSpace(name))
		}
	}
	return uniqueSorted(names)
}

// missingNames returns the names in want that have lacks, ignoring case.
func missingNames(want, have []string) []string {
	known := map[string]bool{}
	for _, n := range have {
		known[strings.ToLower(n)] = true
	}
	var out []string
	for _, n := range want {
		if !known[strings.ToLower(n)] {
			out = append(out, n)
		}
	}
	return out
}

func headerName(line string) string {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
		return ""
	}
	name, _, found := strings.Cut(line, ":")
	if !found {
		return ""
	}
	return strings.TrimSpace(name)
}

func headerNames(raw string) []string {
	var names []string
	for _, line := range strings.Split(raw, "\n") {
		if name := headerName(line); name != "" {
			names = append(names, name)
		}
	}
	return uniqueSorted(names)
}

func formKeys(raw string) []string {
	var rows []formRow
	if json.Unmarshal([]byte(raw), &rows) != nil {
		return nil
	}
	var keys []string
	for _, row := range rows {
		if strings.TrimSpace(row.Key) != "" {
			keys = append(keys, strings.TrimSpace(row.Key))
		}
	}
	return uniqueSorted(keys)
}

// compactBody makes two JSON texts that differ only in whitespace equal.
func compactBody(body string) string {
	body = strings.TrimSpace(body)
	var v any
	if json.Unmarshal([]byte(body), &v) != nil {
		return body
	}
	out, err := json.Marshal(v)
	if err != nil {
		return body
	}
	return string(out)
}

// uniqueSorted de-duplicates names ignoring case, keeping the first spelling.
func uniqueSorted(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if k := strings.ToLower(s); !seen[k] {
			seen[k] = true
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

func sameList(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if strings.ToLower(a[i]) != strings.ToLower(b[i]) {
			return false
		}
	}
	return true
}
