package mcp

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
)

// Masking hides secrets in what a tool returns to an AI client. It works on
// the output only: tools that read and write back (move_requests) read the
// real data through the runner and never see masked text. See docs/mcp.md.

// Redacted replaces a masked value. Input containing it is refused (see
// Server.checkInput) so a masked value can never be written back.
const Redacted = "[REDACTED]"

// baseSecretNames match as substrings of a header, field, or parameter name,
// case-insensitively.
var baseSecretNames = []string{
	"authorization", "cookie", "x-api-key", "api_key", "api-key", "apikey",
	"token", "secret", "password", "passwd", "credential",
	"private_key", "private-key", "access_key", "access-key", "x-auth",
}

// BuiltinSecretNames returns the names that are always masked.
func BuiltinSecretNames() []string { return append([]string{}, baseSecretNames...) }

var (
	// placeholderValue is a value that is only a {{variable}}, optionally after
	// a scheme word: "{{token}}", "Bearer {{token}}".
	placeholderValue = regexp.MustCompile(`^\s*(\w+\s+)?\{\{[^{}]+\}\}\s*$`)
	// jsonStringField matches "key": "value" and keeps the original layout.
	jsonStringField = regexp.MustCompile(`("(?:[^"\\]|\\.)*")(\s*:\s*)("(?:[^"\\]|\\.)*")`)
	urlUserInfo     = regexp.MustCompile(`(://[^/@\s:]+:)([^@/\s]+)(@)`)
	urlQueryParam   = regexp.MustCompile(`([?&])([^=&#\s]+)=([^&#\s]*)`)
)

// Masker decides what is secret.
type Masker struct{ names []string }

// NewMasker adds the user's own names to the built-in list.
func NewMasker(extra []string) *Masker {
	names := append([]string{}, baseSecretNames...)
	for _, e := range extra {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			names = append(names, e)
		}
	}
	return &Masker{names: names}
}

func (m *Masker) sensitive(name string) bool {
	n := strings.ToLower(name)
	for _, s := range m.names {
		if strings.Contains(n, s) {
			return true
		}
	}
	return false
}

// Value masks one value when its name is sensitive, unless it is only a
// {{placeholder}}.
func (m *Masker) Value(name, value string) string {
	if value == "" || !m.sensitive(name) || placeholderValue.MatchString(value) {
		return value
	}
	return Redacted
}

// Headers masks "Name: value" lines.
func (m *Masker) Headers(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		trimmed := strings.TrimRight(value, "\r")
		masked := m.Value(name, strings.TrimSpace(trimmed))
		if masked != strings.TrimSpace(trimmed) {
			lines[i] = name + ": " + masked + value[len(trimmed):]
		}
	}
	return strings.Join(lines, "\n")
}

// URL masks a password in user:pass@host and sensitive query parameters.
func (m *Masker) URL(text string) string {
	text = urlUserInfo.ReplaceAllStringFunc(text, func(s string) string {
		g := urlUserInfo.FindStringSubmatch(s)
		if placeholderValue.MatchString(g[2]) {
			return s
		}
		return g[1] + Redacted + g[3]
	})
	return urlQueryParam.ReplaceAllStringFunc(text, func(s string) string {
		g := urlQueryParam.FindStringSubmatch(s)
		return g[1] + g[2] + "=" + m.Value(g[2], g[3])
	})
}

// Body masks string values under sensitive keys in JSON text, and sensitive
// fields in a form-encoded body. Other bodies are returned unchanged.
func (m *Masker) Body(text string) string {
	trimmed := strings.TrimSpace(text)
	switch {
	case strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "["):
		return jsonStringField.ReplaceAllStringFunc(text, func(s string) string {
			g := jsonStringField.FindStringSubmatch(s)
			var key, value string
			if json.Unmarshal([]byte(g[1]), &key) != nil || json.Unmarshal([]byte(g[3]), &value) != nil {
				return s
			}
			if masked := m.Value(key, value); masked != value {
				return g[1] + g[2] + `"` + Redacted + `"`
			}
			return s
		})
	case trimmed != "" && !strings.ContainsAny(trimmed, " \n\t") && strings.Contains(trimmed, "="):
		pairs := strings.Split(text, "&")
		for i, p := range pairs {
			if k, v, ok := strings.Cut(p, "="); ok {
				pairs[i] = k + "=" + m.Value(k, v)
			}
		}
		return strings.Join(pairs, "&")
	}
	return text
}

// Text masks free text that may be a header block, a URL, or a body.
func (m *Masker) Text(text string) string { return m.Headers(m.URL(m.Body(text))) }

// BodyForm masks the values of sensitive entries in a form body (a JSON array
// of {key, value, ...}).
func (m *Masker) BodyForm(text string) string {
	var entries []map[string]any
	if json.Unmarshal([]byte(text), &entries) != nil {
		return text
	}
	for _, e := range entries {
		key, _ := e["key"].(string)
		if value, ok := e["value"].(string); ok {
			e["value"] = m.Value(key, value)
		}
	}
	out, err := json.Marshal(entries)
	if err != nil {
		return text
	}
	return string(out)
}

// Tree masks a decoded command result in place and returns it. Fields are
// recognised by name: url, request_headers, headers, body, body_form.
func (m *Masker) Tree(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if s, ok := val.(string); ok {
				switch k {
				case "url":
					t[k] = m.URL(s)
				case "request_headers", "headers":
					t[k] = m.Headers(s)
				case "body":
					t[k] = m.Body(s)
				case "body_form":
					t[k] = m.BodyForm(s)
				}
				continue
			}
			t[k] = m.Tree(val)
		}
	case []any:
		for i := range t {
			t[i] = m.Tree(t[i])
		}
	}
	return v
}

// toGeneric turns any result into maps and slices so Tree can walk it. Numbers
// stay exact.
func toGeneric(out any) (any, error) {
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var v any
	if err := decoder.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}
