package scripting

import "strings"

// Headers are a request's or response's headers as scripts see them: ordered
// "Name: value" lines, names matched case-insensitively. Changed reports
// whether a script touched them, so an untouched request keeps its header
// text exactly as typed.
type Headers struct {
	entries []header
	changed bool
}

type header struct{ name, value string }

// ParseHeaders reads "Name: value" lines; lines without a colon are skipped,
// as sending skips them.
func ParseHeaders(text string) *Headers {
	h := &Headers{}
	for _, line := range strings.Split(text, "\n") {
		name, value, found := strings.Cut(strings.TrimSpace(line), ":")
		if !found || strings.TrimSpace(name) == "" {
			continue
		}
		h.entries = append(h.entries, header{strings.TrimSpace(name), strings.TrimSpace(value)})
	}
	return h
}

// Get returns the first value for name, and whether there is one.
func (h *Headers) Get(name string) (string, bool) {
	for _, e := range h.entries {
		if strings.EqualFold(e.name, name) {
			return e.value, true
		}
	}
	return "", false
}

// Set replaces every value for name with one (or adds it at the end).
func (h *Headers) Set(name, value string) {
	h.changed = true
	for i, e := range h.entries {
		if strings.EqualFold(e.name, name) {
			h.entries[i].value = value
			h.removeFrom(name, i+1)
			return
		}
	}
	h.entries = append(h.entries, header{name, value})
}

// Remove deletes every value for name.
func (h *Headers) Remove(name string) {
	h.changed = true
	h.removeFrom(name, 0)
}

func (h *Headers) removeFrom(name string, start int) {
	kept := h.entries[:start]
	for _, e := range h.entries[start:] {
		if !strings.EqualFold(e.name, name) {
			kept = append(kept, e)
		}
	}
	h.entries = kept
}

// All returns the headers as name → value (the first value of a repeated name).
func (h *Headers) All() map[string]string {
	all := make(map[string]string, len(h.entries))
	for _, e := range h.entries {
		if _, seen := all[e.name]; !seen {
			all[e.name] = e.value
		}
	}
	return all
}

// Changed reports whether Set or Remove was called.
func (h *Headers) Changed() bool { return h.changed }

// String writes the headers back as "Name: value" lines.
func (h *Headers) String() string {
	lines := make([]string, len(h.entries))
	for i, e := range h.entries {
		lines[i] = e.name + ": " + e.value
	}
	return strings.Join(lines, "\n")
}
