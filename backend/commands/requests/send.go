package requests

import (
	"bytes"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"snap-rq/backend/commands"
	"snap-rq/backend/commands/responses"
	"snap-rq/backend/cookies"
	"snap-rq/backend/models"
	"snap-rq/backend/secrets"
)

// send.go — execute a request against the outside world. Observable: it
// interpolates, times the request, does the HTTP round trip outside the
// writer lock, and reports elapsed time until it finishes by persisting the
// response and the request's status/recency in one short transaction.

type Send struct{ db *sql.DB }

func NewSend(db *sql.DB) commands.ObservableCommand { return &Send{db: db} }

func (Send) Info() commands.Info {
	return commands.Info{FunctionID: "request.send", Label: "Send Request"}
}

type sendVariable struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

var variablePattern = regexp.MustCompile(`\{\{\s*[a-zA-Z0-9_-]+\s*\}\}`)

func (c Send) Run(input json.RawMessage, obs commands.Observer) {
	var in struct {
		Request   models.HttpRequest `json:"request"`
		Variables []sendVariable     `json:"variables"`
		// EnvironmentID names the environment whose secret variables are
		// filled in here: the frontend never has their values.
		EnvironmentID int64 `json:"environment_id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		obs.Finish(nil, err)
		return
	}

	values := make(map[string]string, len(in.Variables))
	for _, v := range in.Variables {
		values[v.Key] = v.Value
	}

	req := in.Request
	secretValues, err := c.secretsFor(in.EnvironmentID, req)
	if err != nil {
		obs.Finish(nil, err)
		return
	}
	for key, value := range secretValues {
		values[key] = value
	}
	req.URL = interpolate(req.URL, values)
	req.Body = interpolate(req.Body, values)
	req.RequestHeaders = interpolate(req.RequestHeaders, values)

	start := time.Now()
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				obs.Update(map[string]any{"request_id": req.ID, "elapsed_ms": time.Since(start).Milliseconds()})
			}
		}
	}()

	result := c.doHTTP(req, values)
	close(done)
	if result.StatusCode == 0 {
		// A local failure (bad host, timeout…): Go's message quotes the URL,
		// and the message is saved as the response body.
		result.Body = hideSecrets(result.Body, secretValues)
	}
	result.DurationMs = time.Since(start).Milliseconds()

	stored, err := c.persist(req.ID, result)
	if err != nil {
		obs.Finish(nil, err)
		return
	}
	obs.Finish(stored, nil)
}

// secretsFor decrypts the environment's secret variables that the request
// actually uses, and only those. A locked one is an error naming it, rather
// than sending "{{KEY}}" to the server.
func (c Send) secretsFor(environmentID int64, req models.HttpRequest) (map[string]string, error) {
	if environmentID == 0 {
		return nil, nil
	}
	used := map[string]bool{}
	for _, text := range []string{req.URL, req.Body, req.RequestHeaders, req.BodyForm} {
		for _, match := range variablePattern.FindAllString(text, -1) {
			used[strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(match, "{{"), "}}"))] = true
		}
	}
	if len(used) == 0 {
		return nil, nil
	}

	rows, err := c.db.Query(`SELECT key, value FROM environment_variables
		WHERE environment_id = ? AND secret = 1 AND value != ''`, environmentID)
	if err != nil {
		return nil, err
	}
	stored := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			rows.Close()
			return nil, err
		}
		if used[key] {
			stored[key] = value
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	values := make(map[string]string, len(stored))
	for key, encrypted := range stored {
		plain, err := secrets.Default().Decrypt(c.db, encrypted)
		if errors.Is(err, secrets.ErrLocked) {
			return nil, fmt.Errorf("the secret variable %s is locked: restore your recovery key or enter its value again", key)
		}
		if err != nil {
			return nil, fmt.Errorf("decrypting the secret variable %s: %w", key, err)
		}
		values[key] = plain
	}
	return values, nil
}

// hideSecrets replaces secret values in a locally made message.
func hideSecrets(text string, values map[string]string) string {
	for key, value := range values {
		if value != "" {
			text = strings.ReplaceAll(text, value, "{{"+key+"}}")
		}
	}
	return text
}

// interpolate resolves {{ key }} placeholders; unknown keys stay untouched.
func interpolate(text string, values map[string]string) string {
	if len(values) == 0 || !strings.Contains(text, "{{") {
		return text
	}
	return variablePattern.ReplaceAllStringFunc(text, func(match string) string {
		key := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(match, "{{"), "}}"))
		if v, ok := values[key]; ok {
			return v
		}
		return match
	})
}

// formEntry is one row of a multipart form body: a text field or a file.
type formEntry struct {
	Key      string `json:"key"`
	Type     string `json:"type"` // "text" | "file"
	Value    string `json:"value"`
	FilePath string `json:"file_path"`
	FileName string `json:"file_name"`
	Enabled  bool   `json:"enabled"`
}

// buildMultipartBody assembles the wire body from the stored form rows.
// Files are opened from their local paths at send time, never read into the
// database. Text values are interpolated; file paths are not.
func buildMultipartBody(raw string, values map[string]string) (io.Reader, string, error) {
	var entries []formEntry
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &entries); err != nil {
			return nil, "", fmt.Errorf("parsing form body: %w", err)
		}
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for _, entry := range entries {
		if !entry.Enabled || entry.Key == "" {
			continue
		}
		if entry.Type == "file" {
			if err := writeFilePart(writer, entry); err != nil {
				return nil, "", err
			}
		} else {
			if err := writer.WriteField(entry.Key, interpolate(entry.Value, values)); err != nil {
				return nil, "", fmt.Errorf("writing field %s: %w", entry.Key, err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return &buf, writer.FormDataContentType(), nil
}

// writeFilePart streams one file into the multipart body, keyed by the entry
// key with the file's base name as the part filename.
func writeFilePart(writer *multipart.Writer, entry formEntry) error {
	filename := entry.FileName
	if filename == "" {
		filename = filepath.Base(entry.FilePath)
	}
	part, err := writer.CreateFormFile(entry.Key, filename)
	if err != nil {
		return fmt.Errorf("creating file part %s: %w", entry.Key, err)
	}
	file, err := os.Open(entry.FilePath)
	if err != nil {
		return fmt.Errorf("opening file %s: %w", entry.FilePath, err)
	}
	defer file.Close()
	if _, err := io.Copy(part, file); err != nil {
		return fmt.Errorf("reading file %s: %w", entry.FilePath, err)
	}
	return nil
}

// ensureScheme prepends http:// when a URL has no scheme, so requests with
// schemeless URLs (typed by hand or imported) still send.
func ensureScheme(u string) string {
	if u == "" || strings.Contains(u, "://") {
		return u
	}
	return "http://" + u
}

// headerHas reports whether a request's header block already contains the
// given header name, so auto-generated headers never clobber the user's.
func headerHas(headers, name string) bool {
	for _, line := range strings.Split(headers, "\n") {
		key, _, found := strings.Cut(line, ":")
		if found && strings.EqualFold(strings.TrimSpace(key), name) {
			return true
		}
	}
	return false
}

// doHTTP runs the request and returns a response; network and build errors
// become a status-0 response body instead of a Go error.
func (c Send) doHTTP(req models.HttpRequest, values map[string]string) models.HttpResponse {
	fail := func(body string) models.HttpResponse {
		return models.HttpResponse{RequestID: req.ID, StatusCode: 0, Body: body}
	}

	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	req.URL = ensureScheme(req.URL)

	var bodyReader io.Reader
	var contentType string
	if normalizeBodyType(req.BodyType) == "form-data" {
		reader, ct, err := buildMultipartBody(req.BodyForm, values)
		if err != nil {
			return fail(err.Error())
		}
		bodyReader, contentType = reader, ct
	} else if req.Body != "" {
		bodyReader = strings.NewReader(req.Body)
	}

	httpReq, err := http.NewRequest(method, req.URL, bodyReader)
	if err != nil {
		return fail(fmt.Sprintf("building request: %v", err))
	}
	if contentType != "" && !headerHas(req.RequestHeaders, "Content-Type") {
		httpReq.Header.Set("Content-Type", contentType)
	}
	for _, line := range strings.Split(req.RequestHeaders, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		httpReq.Header.Set(strings.TrimSpace(key), strings.TrimSpace(value))
	}

	client, err := c.httpClient(req.ProjectID)
	if err != nil {
		return fail(fmt.Sprintf("building client: %v", err))
	}
	httpResp, err := client.Do(httpReq)
	if err != nil {
		return fail(fmt.Sprintf("request failed: %v", err))
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return fail(fmt.Sprintf("reading response body: %v", err))
	}

	var respHeaders strings.Builder
	for name, values := range httpResp.Header {
		for _, value := range values {
			respHeaders.WriteString(fmt.Sprintf("%s: %s\n", name, value))
		}
	}
	return models.HttpResponse{
		RequestID:  req.ID,
		StatusCode: httpResp.StatusCode,
		Headers:    strings.TrimSpace(respHeaders.String()),
		Body:       string(respBody),
	}
}

// persist stores the response and updates the request's status and recency
// in one short transaction.
func (c Send) persist(reqID int64, result models.HttpResponse) (models.HttpResponse, error) {
	tx, err := c.db.Begin()
	if err != nil {
		return models.HttpResponse{}, err
	}
	defer tx.Rollback()

	stored, err := responses.InsertResponse(tx, result)
	if err != nil {
		return models.HttpResponse{}, err
	}
	if _, err := tx.Exec(`
		UPDATE http_requests SET status_code = ?, response_id = ?, last_used_at = ?
		WHERE id = ?`,
		stored.StatusCode, stored.ID, time.Now().UTC().UnixNano(), reqID,
	); err != nil {
		return models.HttpResponse{}, err
	}
	if err := tx.Commit(); err != nil {
		return models.HttpResponse{}, err
	}
	return stored, nil
}

// httpClient builds the client from the user's settings; certificate
// validation is skipped only when the user explicitly disabled it. Unless the
// cookie jar is switched off, the project's jar stores the cookies servers set
// (on every redirect hop too) and sends them back on matching requests.
func (c Send) httpClient(projectID int64) (*http.Client, error) {
	timeoutSeconds, validateSSL, useCookieJar := 30, true, true
	var raw string
	if err := c.db.QueryRow(
		`SELECT value FROM app_settings WHERE key = 'request_timeout_seconds'`,
	).Scan(&raw); err == nil {
		_ = json.Unmarshal([]byte(raw), &timeoutSeconds)
	}
	if err := c.db.QueryRow(
		`SELECT value FROM app_settings WHERE key = 'validate_ssl_certificates'`,
	).Scan(&raw); err == nil {
		_ = json.Unmarshal([]byte(raw), &validateSSL)
	}

	if err := c.db.QueryRow(
		`SELECT value FROM app_settings WHERE key = 'use_cookie_jar'`,
	).Scan(&raw); err == nil {
		_ = json.Unmarshal([]byte(raw), &useCookieJar)
	}

	client := &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second}
	if useCookieJar && projectID > 0 {
		client.Jar = cookies.New(c.db, projectID)
	}
	if validateSSL {
		return client, nil
	}
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return client, nil
	}
	cloned := transport.Clone()
	cloned.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	client.Transport = cloned
	return client, nil
}
