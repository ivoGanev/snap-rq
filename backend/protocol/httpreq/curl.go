package httpreq

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"snap-rq/backend/models"
)

// Curl writes the request as it goes out: the copy matches what is sent.
func (o Outgoing) Curl() string {
	names := make([]string, 0, len(o.Header))
	for name := range o.Header {
		names = append(names, name)
	}
	slices.Sort(names)
	var lines []string
	for _, name := range names {
		for _, value := range o.Header[name] {
			lines = append(lines, name+": "+value)
		}
	}
	req := models.HttpRequest{Method: o.Method, URL: o.URL, RequestHeaders: strings.Join(lines, "\n"), Body: o.Body}
	if o.FormData {
		form, _ := json.Marshal(o.Form)
		req.BodyType, req.BodyForm = "form-data", string(form)
	}
	return Curl(req)
}

// Curl converts a request into an equivalent curl command string.
// The output uses --data-raw for the body and -H for headers. The method is
// explicit when it is not GET or when a body is present. The URL goes in as
// stored, so importing the command gives back the same URL.
func Curl(req models.HttpRequest) string {
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}

	parts := []string{"curl"}

	isForm := req.BodyType == "form-data"

	if method != http.MethodGet || req.Body != "" || isForm {
		parts = append(parts, "-X", method)
	}

	parts = append(parts, shellQuote(req.URL))

	for _, line := range strings.Split(req.RequestHeaders, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if strings.EqualFold(key, "Host") || strings.EqualFold(key, "Content-Length") {
			continue
		}
		parts = append(parts, "-H", shellQuote(key+": "+value))
	}

	if isForm {
		for _, f := range curlFormParts(req.BodyForm) {
			parts = append(parts, "-F", shellQuote(f))
		}
	} else if req.Body != "" {
		parts = append(parts, "--data-raw", shellQuote(req.Body))
	}

	return strings.Join(parts, " ")
}

// curlFormParts renders the stored form rows as curl -F arguments. Files use
// the key=@path form; text uses key=value.
func curlFormParts(raw string) []string {
	var entries []models.FormField
	if json.Unmarshal([]byte(raw), &entries) != nil {
		return nil
	}
	var parts []string
	for _, e := range entries {
		if !e.Enabled || e.Key == "" {
			continue
		}
		if e.Type == "file" {
			parts = append(parts, e.Key+"=@"+e.FilePath)
		} else {
			parts = append(parts, e.Key+"="+e.Value)
		}
	}
	return parts
}

// shellQuote returns a single-quoted shell word, falling back to double quotes
// when the value contains single quotes.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.Contains(s, "'") {
		return "'" + s + "'"
	}
	// Use double quotes and escape a minimal set of characters.
	quoted := strings.ReplaceAll(s, "\\", "\\\\")
	quoted = strings.ReplaceAll(quoted, "\"", "\\\"")
	quoted = strings.ReplaceAll(quoted, "$", "\\$")
	quoted = strings.ReplaceAll(quoted, "`", "\\`")
	return "\"" + quoted + "\""
}
