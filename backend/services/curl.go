package services

import (
	"fmt"
	"net/http"
	"strings"

	"snap-rq/backend/models"
)

// RequestToCurl converts an HttpRequest into an equivalent curl command string.
// The output uses --data-raw for the body and -H for headers. The method is
// explicit when it is not GET or when a body is present.
func RequestToCurl(req models.HttpRequest) string {
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}

	parts := []string{"curl"}

	if method != http.MethodGet || req.Body != "" {
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

	if req.Body != "" {
		parts = append(parts, "--data-raw", shellQuote(req.Body))
	}

	return strings.Join(parts, " ")
}

// CurlToRequest parses a curl command string and returns an HttpRequest.
// It supports -X/--request, -H/--header, -d/--data/--data-raw/--data-binary,
// and a single positional URL. The collectionID is attached to the result.
func CurlToRequest(collectionID int64, curl string) (models.HttpRequest, error) {
	tokens, err := tokenize(curl)
	if err != nil {
		return models.HttpRequest{}, fmt.Errorf("tokenizing curl: %w", err)
	}
	if len(tokens) == 0 {
		return models.HttpRequest{}, fmt.Errorf("empty curl command")
	}
	if strings.ToLower(tokens[0]) != "curl" {
		return models.HttpRequest{}, fmt.Errorf("curl command must start with 'curl'")
	}

	req := models.HttpRequest{
		CollectionID: collectionID,
		Method:       http.MethodGet,
		Name:         "Imported curl",
	}

	var headers []string
	i := 1
	for i < len(tokens) {
		tok := tokens[i]

		switch tok {
		case "-X", "--request":
			if i+1 >= len(tokens) {
				return models.HttpRequest{}, fmt.Errorf("missing value for %s", tok)
			}
			req.Method = strings.ToUpper(tokens[i+1])
			i += 2
		case "-H", "--header":
			if i+1 >= len(tokens) {
				return models.HttpRequest{}, fmt.Errorf("missing value for %s", tok)
			}
			headers = append(headers, tokens[i+1])
			i += 2
		case "-d", "--data", "--data-raw", "--data-binary":
			if i+1 >= len(tokens) {
				return models.HttpRequest{}, fmt.Errorf("missing value for %s", tok)
			}
			req.Body = tokens[i+1]
			i += 2
		default:
			if strings.HasPrefix(tok, "-") {
				// Unknown flag; skip its value if it looks like a joined flag-value
				// (e.g. -XPOST) or a standalone flag.
				if !strings.Contains(tok, "=") && len(tok) > 2 && !strings.HasPrefix(tok, "--") {
					// Joined short flag like -XPOST
					req.Method = strings.ToUpper(tok[2:])
				} else {
					return models.HttpRequest{}, fmt.Errorf("unsupported curl flag: %s", tok)
				}
				i++
			} else if req.URL == "" {
				req.URL = tok
				i++
			} else {
				return models.HttpRequest{}, fmt.Errorf("unexpected token: %s", tok)
			}
		}
	}

	if req.URL == "" {
		return models.HttpRequest{}, fmt.Errorf("curl command is missing a URL")
	}

	if req.Body != "" && req.Method == http.MethodGet {
		req.Method = http.MethodPost
	}

	for i, h := range headers {
		headers[i] = normalizeHeader(h)
	}
	req.RequestHeaders = strings.Join(headers, "\n")

	return req, nil
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

// tokenize splits a command string into tokens respecting single and double
// quotes and backslash escaping.
func tokenize(input string) ([]string, error) {
	var tokens []string
	var current strings.Builder
	var quote rune
	escaped := false

	for _, r := range input {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}

		if r == '\\' {
			escaped = true
			continue
		}

		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}
			continue
		}

		switch r {
		case '\'', '"':
			quote = r
		case ' ', '\t', '\n', '\r':
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}

	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote")
	}
	if escaped {
		return nil, fmt.Errorf("trailing backslash")
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return tokens, nil
}

// normalizeHeader ensures a header string contains exactly one colon with a
// single space after it.
func normalizeHeader(h string) string {
	key, value, found := strings.Cut(h, ":")
	if !found {
		return h
	}
	return strings.TrimSpace(key) + ": " + strings.TrimSpace(value)
}
