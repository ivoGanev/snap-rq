package builder

import (
	"fmt"
	"net/http"
	"strings"
)

// detailsMarker separates the friendly text of a failed send from the raw
// error; the response view folds what follows it under "Details".
const detailsMarker = "\n\nDetails: "

// WithDetails puts the friendly text first and keeps the raw error after it.
func WithDetails(friendly, raw string) string {
	if friendly == "" {
		return raw
	}
	return friendly + detailsMarker + raw
}

// headerLineError is a header line the server could never receive as a header.
type headerLineError struct {
	Line int
	Text string
}

func (e *headerLineError) Error() string {
	return fmt.Sprintf("Header line %d isn't a header: %s\n"+
		"Headers go one per line, as  Name: value  (for example  Accept: application/json).", e.Line, e.Text)
}

// parseHeaders reads "Name: value" lines; blank lines are skipped.
func parseHeaders(text string) (http.Header, error) {
	headers := http.Header{}
	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		name, value, found := strings.Cut(line, ":")
		name = strings.TrimSpace(name)
		if !found || !validHeaderName(name) {
			return nil, &headerLineError{Line: i + 1, Text: line}
		}
		headers.Set(name, strings.TrimSpace(value))
	}
	return headers, nil
}

// validHeaderName reports whether name is a token (RFC 9110): letters,
// digits and !#$%&'*+-.^_`|~.
func validHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		ok := r < 128 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			strings.ContainsRune("!#$%&'*+-.^_`|~", r))
		if !ok {
			return false
		}
	}
	return true
}
