package requests

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"snap-rq/backend/commands"
	"snap-rq/backend/commands/responses"
	"snap-rq/backend/cookies"
	"snap-rq/backend/models"
	"snap-rq/backend/scripting"
	"snap-rq/backend/secrets"
)

// send.go — execute a request against the outside world. Observable: it
// fills in variables, runs the request's scripts' beforeRequest hooks, fills
// in the placeholders they wrote, times the
// request, does the HTTP round trip outside the writer lock, runs the
// afterResponse hooks, and reports elapsed time until it finishes by
// persisting the response (with the scripts' log) and the request's
// status/recency in one short transaction.

type Send struct{ db *sql.DB }

func NewSend(db *sql.DB) commands.ObservableCommand { return &Send{db: db} }

func (Send) Info() commands.Info {
	return commands.Info{FunctionID: "request.send", Label: "Send Request"}
}

type sendVariable struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

var errCancelled = errors.New("request cancelled")

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

	plain := make(map[string]string, len(in.Variables))
	for _, v := range in.Variables {
		plain[v.Key] = v.Value
	}
	req := in.Request

	start := time.Now()
	done := make(chan struct{})
	stopTicking := sync.OnceFunc(func() { close(done) })
	defer stopTicking()
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

	// Variables are filled in before the scripts, so they see the request as
	// it will go out, and again after them, for placeholders a script wrote
	// (e.g. 'Bearer {{TOKEN}}').
	secretValues, err := c.secretsFor(in.EnvironmentID, req)
	if err != nil {
		obs.Finish(nil, err)
		return
	}
	values := make(map[string]string, len(plain)+len(secretValues))
	maps.Copy(values, plain)
	maps.Copy(values, secretValues)
	req = fillIn(req, values)

	scripts, err := c.scriptsFor(req.ID)
	if err != nil {
		obs.Finish(nil, err)
		return
	}
	env := &scriptEnv{send: c, environmentID: in.EnvironmentID, plain: plain, revealed: map[string]string{}}
	// Every secret value a script could print: the ones sent and the ones
	// scripts read.
	mask := func(text string) string {
		return hideSecrets(hideSecrets(text, secretValues), env.revealed)
	}
	ctx := obs.Context
	if ctx == nil {
		ctx = context.Background()
	}
	session := scripting.NewSession(ctx, scripts, env)
	if len(scripts) > 0 {
		scripted := scripting.Request{
			Method: req.Method, URL: req.URL, Body: req.Body,
			Headers: scripting.ParseHeaders(req.RequestHeaders),
		}
		skipped, err := session.BeforeRequest(&scripted)
		if ctx.Err() != nil {
			obs.Finish(nil, errCancelled)
			return
		}
		req.Method, req.URL, req.Body = scripted.Method, scripted.URL, scripted.Body
		if scripted.Headers.Changed() {
			req.RequestHeaders = scripted.Headers.String()
		}
		if err != nil || skipped != "" {
			// Not sent: saved like any failed request, with the scripts' log.
			reason := skipped
			if err != nil {
				reason = err.Error()
			}
			c.finish(obs, models.HttpResponse{
				RequestID:  req.ID,
				Body:       mask(reason),
				DurationMs: time.Since(start).Milliseconds(),
				ScriptLog:  session.Log(mask),
			})
			return
		}

		more, err := c.secretsFor(in.EnvironmentID, req)
		if err != nil {
			obs.Finish(nil, err)
			return
		}
		maps.Copy(secretValues, more)
		maps.Copy(values, more)
		req = fillIn(req, values)
	}

	sent := time.Now()
	result := c.doHTTP(ctx, req, values)
	result.DurationMs = time.Since(sent).Milliseconds()
	if result.StatusCode == 0 {
		// A local failure (bad host, timeout…): Go's message quotes the URL,
		// and the message is saved as the response body.
		result.Body = mask(result.Body)
	} else if len(scripts) > 0 {
		session.AfterResponse(&scripting.Response{
			Status: result.StatusCode, Headers: scripting.ParseHeaders(result.Headers),
			Body: result.Body, DurationMs: result.DurationMs,
		})
	}
	stopTicking()
	// A cancelled send leaves no response behind.
	if ctx.Err() != nil {
		obs.Finish(nil, errCancelled)
		return
	}

	result.ScriptLog = session.Log(mask)
	c.finish(obs, result)
}

// fillIn resolves the request's {{placeholders}} (form fields are filled in
// when the multipart body is built).
func fillIn(req models.HttpRequest, values map[string]string) models.HttpRequest {
	req.URL = interpolate(req.URL, values)
	req.Body = interpolate(req.Body, values)
	req.RequestHeaders = interpolate(req.RequestHeaders, values)
	return req
}

// finish saves the response and ends the run with it.
func (c Send) finish(obs commands.Observer, result models.HttpResponse) {
	stored, err := c.persist(result.RequestID, result)
	if err != nil {
		obs.Finish(nil, err)
		return
	}
	obs.Finish(stored, nil)
}

// scriptsFor lists the request's enabled scripts with code, in run order.
func (c Send) scriptsFor(requestID int64) ([]scripting.Script, error) {
	if requestID == 0 {
		return nil, nil
	}
	rows, err := c.db.Query(`
		SELECT s.id, s.name, s.content FROM request_scripts rs
		JOIN scripts s ON s.id = rs.script_id
		WHERE rs.request_id = ? AND rs.enabled = 1 AND TRIM(s.content) != ''
		ORDER BY rs.run_order, rs.id`, requestID)
	if err != nil {
		return nil, fmt.Errorf("loading the request's scripts: %w", err)
	}
	defer rows.Close()
	var scripts []scripting.Script
	for rows.Next() {
		var s scripting.Script
		if err := rows.Scan(&s.ID, &s.Name, &s.Source); err != nil {
			return nil, err
		}
		scripts = append(scripts, s)
	}
	return scripts, rows.Err()
}

// scriptEnv is ctx.env for scripts: the environment's plain variables as the
// frontend sent them, and its secret ones, decrypted only when a script asks
// (and remembered, so the run log can mask them).
type scriptEnv struct {
	send          Send
	environmentID int64
	plain         map[string]string
	revealed      map[string]string
}

func (e *scriptEnv) Get(key string) (string, bool) {
	if v, ok := e.plain[key]; ok {
		return v, true
	}
	if v, ok := e.revealed[key]; ok {
		return v, true
	}
	if e.environmentID == 0 {
		return "", false
	}
	var encrypted string
	if err := e.send.db.QueryRow(`SELECT value FROM environment_variables
		WHERE environment_id = ? AND key = ? AND secret = 1 AND value != ''`,
		e.environmentID, key).Scan(&encrypted); err != nil {
		return "", false
	}
	value, err := secrets.Default().Decrypt(e.send.db, encrypted)
	if err != nil {
		return "", false // locked or damaged: as if unset
	}
	e.revealed[key] = value
	return value, true
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

// secretMask is what a secret value is shown as. Not "{{KEY}}": that would
// look like a variable that wasn't filled in.
const secretMask = "•••••"

// hideSecrets replaces secret values in text meant for display (a failed
// request's message, a script's log).
func hideSecrets(text string, values map[string]string) string {
	for _, value := range values {
		if value != "" {
			text = strings.ReplaceAll(text, value, secretMask)
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
func (c Send) doHTTP(ctx context.Context, req models.HttpRequest, values map[string]string) models.HttpResponse {
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
		contentType = "application/json"
		if normalizeBodyType(req.BodyType) == "text" {
			contentType = "text/plain"
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, req.URL, bodyReader)
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

// persist stores the response and updates the request's status, recency and
// usage score (see usage.go) in one short transaction.
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
	var score float64
	var lastUsed int64
	if err := tx.QueryRow(`SELECT use_score, last_used_at FROM http_requests WHERE id = ?`, reqID).
		Scan(&score, &lastUsed); err != nil {
		return models.HttpResponse{}, fmt.Errorf("reading the request's usage: %w", err)
	}
	now := time.Now()
	if _, err := tx.Exec(`
		UPDATE http_requests SET status_code = ?, response_id = ?, last_used_at = ?, use_score = ?
		WHERE id = ?`,
		stored.StatusCode, stored.ID, now.UTC().UnixNano(), fadedScore(score, lastUsed, now)+1, reqID,
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
