package requests

import (
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"snap-rq/backend/commands"
	"snap-rq/backend/commands/responses"
	"snap-rq/backend/models"
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

	result := c.doHTTP(req)
	close(done)
	result.DurationMs = time.Since(start).Milliseconds()

	stored, err := c.persist(req.ID, result)
	if err != nil {
		obs.Finish(nil, err)
		return
	}
	obs.Finish(stored, nil)
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

// doHTTP runs the request and returns a response; network and build errors
// become a status-0 response body instead of a Go error.
func (c Send) doHTTP(req models.HttpRequest) models.HttpResponse {
	fail := func(body string) models.HttpResponse {
		return models.HttpResponse{RequestID: req.ID, StatusCode: 0, Body: body}
	}

	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	var bodyReader io.Reader
	if req.Body != "" {
		bodyReader = strings.NewReader(req.Body)
	}
	httpReq, err := http.NewRequest(method, req.URL, bodyReader)
	if err != nil {
		return fail(fmt.Sprintf("building request: %v", err))
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

	client, err := c.httpClient()
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
// validation is skipped only when the user explicitly disabled it.
func (c Send) httpClient() (*http.Client, error) {
	timeoutSeconds, validateSSL := 30, true
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

	client := &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second}
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
