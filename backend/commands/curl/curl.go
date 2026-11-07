package curl

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"snap-rq/backend/models"
)

// RequestToCurl converts an HttpRequest into an equivalent curl command string.
// The output uses --data-raw for the body and -H for headers. The method is
// explicit when it is not GET or when a body is present. When there is no body
// and the URL has a query string, the query parameters are emitted as -G plus
// --data-urlencode so the command stays readable.
func RequestToCurl(req models.HttpRequest) string {
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}

	parts := []string{"curl"}

	isForm := req.BodyType == "form-data"

	baseURL := req.URL
	var queryArgs []string
	if req.Body == "" && !isForm {
		if base, args, ok := splitQueryForCurl(req.URL); ok {
			baseURL = base
			queryArgs = args
			parts = append(parts, "-G")
		}
	}

	if method != http.MethodGet || req.Body != "" || isForm {
		parts = append(parts, "-X", method)
	}

	parts = append(parts, shellQuote(baseURL))

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

	for _, arg := range queryArgs {
		parts = append(parts, "--data-urlencode", shellQuote(arg))
	}

	return strings.Join(parts, " ")
}

// formRow is one multipart form entry, shared between curl render and parse.
type formRow struct {
	Key      string `json:"key"`
	Type     string `json:"type"` // "text" | "file"
	Value    string `json:"value"`
	FilePath string `json:"file_path"`
	FileName string `json:"file_name"`
	Enabled  bool   `json:"enabled"`
}

// curlFormParts renders the stored form rows as curl -F arguments. Files use
// the key=@path form; text uses key=value.
func curlFormParts(raw string) []string {
	var entries []formRow
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

// curlFormRow parses one -F argument into a form row. "key=@file" becomes a
// file row, everything else a text row.
func curlFormRow(arg string) formRow {
	row := formRow{Enabled: true}
	if name, value, found := strings.Cut(arg, "="); found {
		row.Key = name
		if strings.HasPrefix(value, "@") {
			row.Type = "file"
			row.FilePath = strings.TrimPrefix(value, "@")
		} else {
			row.Type = "text"
			row.Value = value
		}
	} else {
		row.Key = arg
		row.Type = "text"
	}
	return row
}

// splitQueryForCurl splits a URL into its base and a list of "name=value"
// arguments (with decoded values) suitable for --data-urlencode. It reports
// false when the URL has no query string or the query cannot be represented
// faithfully (fragment, parameter without "=", or a name that needs encoding).
func splitQueryForCurl(rawURL string) (string, []string, bool) {
	base, rawQuery, found := strings.Cut(rawURL, "?")
	if !found || rawQuery == "" || strings.Contains(rawURL, "#") {
		return "", nil, false
	}

	var args []string
	for _, pair := range strings.Split(rawQuery, "&") {
		if pair == "" {
			continue
		}
		name, value, hasEq := strings.Cut(pair, "=")
		if !hasEq || name == "" {
			return "", nil, false
		}
		decodedName, err := url.QueryUnescape(name)
		if err != nil || decodedName != name {
			return "", nil, false
		}
		decodedValue, err := url.QueryUnescape(value)
		if err != nil {
			return "", nil, false
		}
		args = append(args, name+"="+decodedValue)
	}
	if len(args) == 0 {
		return "", nil, false
	}
	return base, args, true
}

// CurlToRequest parses a curl command string and returns an HttpRequest.
// It supports -X/--request, -H/--header, -d/--data/--data-raw/--data-binary,
// --data-urlencode, -G/--get, and a single positional URL. The collectionID is
// attached to the result.
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
	var dataParts []string
	var formParts []string
	getMode := false
	methodSet := false
	i := 1
	for i < len(tokens) {
		tok := tokens[i]

		switch tok {
		case "-X", "--request":
			if i+1 >= len(tokens) {
				return models.HttpRequest{}, fmt.Errorf("missing value for %s", tok)
			}
			req.Method = strings.ToUpper(tokens[i+1])
			methodSet = true
			i += 2
		case "-G", "--get":
			getMode = true
			i++
		case "-H", "--header":
			if i+1 >= len(tokens) {
				return models.HttpRequest{}, fmt.Errorf("missing value for %s", tok)
			}
			headers = append(headers, tokens[i+1])
			i += 2
		case "-d", "--data", "--data-raw", "--data-binary", "--data-ascii":
			if i+1 >= len(tokens) {
				return models.HttpRequest{}, fmt.Errorf("missing value for %s", tok)
			}
			dataParts = append(dataParts, tokens[i+1])
			i += 2
		case "--data-urlencode":
			if i+1 >= len(tokens) {
				return models.HttpRequest{}, fmt.Errorf("missing value for %s", tok)
			}
			dataParts = append(dataParts, urlencodeData(tokens[i+1]))
			i += 2
		case "-F", "--form":
			if i+1 >= len(tokens) {
				return models.HttpRequest{}, fmt.Errorf("missing value for %s", tok)
			}
			formParts = append(formParts, tokens[i+1])
			i += 2
		case "--url":
			if i+1 >= len(tokens) {
				return models.HttpRequest{}, fmt.Errorf("missing value for %s", tok)
			}
			req.URL = tokens[i+1]
			i += 2
		case "-b", "--cookie":
			if i+1 >= len(tokens) {
				return models.HttpRequest{}, fmt.Errorf("missing value for %s", tok)
			}
			headers = append(headers, "Cookie: "+tokens[i+1])
			i += 2
		default:
			if strings.HasPrefix(tok, "-X") && len(tok) > 2 && !strings.HasPrefix(tok, "--") {
				// Joined short flag like -XPOST.
				req.Method = strings.ToUpper(tok[2:])
				methodSet = true
				i++
			} else if strings.HasPrefix(tok, "-") {
				// Any other flag (e.g. -s, -v, -k, --compressed, -o file) has no
				// effect on the imported request, so it's ignored rather than
				// requiring every curl flag to be explicitly whitelisted.
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

	if len(dataParts) > 0 {
		data := strings.Join(dataParts, "&")
		if getMode {
			sep := "?"
			if strings.Contains(req.URL, "?") {
				sep = "&"
				if strings.HasSuffix(req.URL, "?") || strings.HasSuffix(req.URL, "&") {
					sep = ""
				}
			}
			req.URL += sep + data
		} else {
			req.Body = data
		}
	}

	if len(formParts) > 0 {
		rows := make([]formRow, 0, len(formParts))
		for _, arg := range formParts {
			rows = append(rows, curlFormRow(arg))
		}
		if raw, err := json.Marshal(rows); err == nil {
			req.BodyForm = string(raw)
			req.BodyType = "form-data"
		}
	}

	if (req.Body != "" || req.BodyForm != "") && req.Method == http.MethodGet && !methodSet {
		req.Method = http.MethodPost
	}

	for i, h := range headers {
		headers[i] = normalizeHeader(h)
	}
	req.RequestHeaders = strings.Join(headers, "\n")

	return req, nil
}

// urlencodeData implements curl's --data-urlencode value forms: "content",
// "=content" and "name=content". The content part is percent-encoded.
func urlencodeData(s string) string {
	enc := func(v string) string {
		return strings.ReplaceAll(url.QueryEscape(v), "+", "%20")
	}
	if name, content, found := strings.Cut(s, "="); found {
		if name == "" {
			return enc(content)
		}
		return name + "=" + enc(content)
	}
	return enc(s)
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
			if r == '\r' {
				// Wait for the following '\n' to complete a CRLF line continuation.
				continue
			}
			if r == '\n' {
				// Backslash-newline is a line continuation; drop both.
				escaped = false
				continue
			}
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
