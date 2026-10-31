package runnables

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
	"snap-rq/backend/store"
)

// ---------------------------------------------------------------------------
// Reads
// ---------------------------------------------------------------------------

// ListResponsesForRequest returns a request's response history, newest
// first.
type ListResponsesForRequest struct{}

func (ListResponsesForRequest) Info() commands.Info {
	return commands.Info{FunctionID: "response.list_for_request", Label: "List Responses", ReadOnly: true}
}

func (ListResponsesForRequest) Run(db store.Session, input json.RawMessage) (any, error) {
	var payload struct {
		RequestID int64 `json:"request_id"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, err
	}
	rows, err := db.Query(
		`SELECT id, request_id, COALESCE(headers, ''), status_code, COALESCE(body, ''), created_at, duration_ms
		 FROM responses WHERE request_id = ? ORDER BY id DESC`, payload.RequestID)
	if err != nil {
		return nil, fmt.Errorf("listing responses: %w", err)
	}
	defer rows.Close()

	var responses []models.HttpResponse
	for rows.Next() {
		var resp models.HttpResponse
		if err := rows.Scan(&resp.ID, &resp.RequestID, &resp.Headers, &resp.StatusCode,
			&resp.Body, &resp.CreatedAt, &resp.DurationMs); err != nil {
			return nil, fmt.Errorf("reading a response: %w", err)
		}
		responses = append(responses, resp)
	}
	return responses, rows.Err()
}

// ---------------------------------------------------------------------------
// The send flow. Sends are never undoable (you cannot un-send a request);
// response history is append-only; usage is telemetry. All of it still
// runs through the engine so every operation follows the same path.
// ---------------------------------------------------------------------------

// SendRequest executes a request against the outside world. It writes
// nothing to the database (the caller persists the outcome via
// response.save / response.update), so it runs without the writer lock —
// a slow send never blocks a rename.
type SendRequest struct{}

func (SendRequest) Info() commands.Info {
	return commands.Info{FunctionID: "request.send", Label: "Send Request", ReadOnly: true}
}

func (SendRequest) Run(db store.Session, input json.RawMessage) (any, error) {
	var req models.HttpRequest
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	if req.ID == 0 {
		return nil, fmt.Errorf("request id is required")
	}
	return sendHTTP(db, req)
}

func sendHTTP(db store.Session, req models.HttpRequest) (models.HttpResponse, error) {
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
		return models.HttpResponse{
			RequestID: req.ID, StatusCode: 0,
			Body: fmt.Errorf("building request: %w", err).Error(),
		}, nil
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

	client, err := httpClient(db)
	if err != nil {
		return models.HttpResponse{}, err
	}
	start := time.Now()
	httpResp, err := client.Do(httpReq)
	duration := time.Since(start).Milliseconds()
	if err != nil {
		return models.HttpResponse{
			RequestID: req.ID, StatusCode: 0,
			Body:       fmt.Errorf("request failed: %w", err).Error(),
			DurationMs: duration,
		}, nil
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return models.HttpResponse{
			RequestID: req.ID, StatusCode: 0,
			Body:       fmt.Errorf("reading response body: %w", err).Error(),
			DurationMs: duration,
		}, nil
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
		DurationMs: duration,
	}, nil
}

// httpClient builds the HTTP client from the user's settings. Certificate
// validation is skipped only when the user explicitly disabled it.
func httpClient(db store.Session) (*http.Client, error) {
	timeoutSeconds := 30
	validateSSL := true

	var raw string
	if err := db.QueryRow(`SELECT value FROM app_settings WHERE key = 'request_timeout_seconds'`).Scan(&raw); err == nil {
		_ = json.Unmarshal([]byte(raw), &timeoutSeconds)
	}
	if err := db.QueryRow(`SELECT value FROM app_settings WHERE key = 'validate_ssl_certificates'`).Scan(&raw); err == nil {
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

// SaveResponse stores one response and trims the history to the newest
// ten entries per request. The request row is not touched here.
type SaveResponse struct{}

func (SaveResponse) Info() commands.Info {
	return commands.Info{FunctionID: "response.save", Label: "Save Response"}
}

func (SaveResponse) Run(db store.Session, input json.RawMessage) (any, error) {
	var resp models.HttpResponse
	if err := json.Unmarshal(input, &resp); err != nil {
		return nil, err
	}
	return insertResponse(db, resp)
}

// SaveResponses stores a batch of responses in one transaction and points
// each originating request at its newest status — the durable write of a
// multi-request send.
type SaveResponses struct{}

func (SaveResponses) Info() commands.Info {
	return commands.Info{
		FunctionID: "response.save_bulk",
		Label:      "Save Responses",
		// The send flow owns these two columns; user-edit commands never
		// write them, so an undo of a user edit can never clobber a send.
		Columns: []string{"status_code", "response_id"},
	}
}

func (SaveResponses) Run(db store.Session, input json.RawMessage) (any, error) {
	var payload struct {
		Responses []models.HttpResponse `json:"responses"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, err
	}
	if len(payload.Responses) == 0 {
		return nil, nil
	}

	saved := make([]models.HttpResponse, 0, len(payload.Responses))
	for _, resp := range payload.Responses {
		stored, err := insertResponse(db, resp)
		if err != nil {
			return nil, fmt.Errorf("saving response for request %d: %w", resp.RequestID, err)
		}
		if err := db.Update("http_requests", resp.RequestID, map[string]any{
			"status_code": stored.StatusCode,
			"response_id": stored.ID,
		}); err != nil {
			return nil, err
		}
		saved = append(saved, stored)
	}
	return saved, nil
}

func insertResponse(db store.Session, resp models.HttpResponse) (models.HttpResponse, error) {
	if resp.CreatedAt == "" {
		resp.CreatedAt = time.Now().UTC().Format("2006-01-02 15:04:05")
	}
	id, err := db.Insert("responses", map[string]any{
		"request_id":  resp.RequestID,
		"headers":     resp.Headers,
		"status_code": resp.StatusCode,
		"body":        resp.Body,
		"created_at":  resp.CreatedAt,
		"duration_ms": resp.DurationMs,
	})
	if err != nil {
		return models.HttpResponse{}, err
	}
	resp.ID = id

	// History is append-only and capped: keep the newest ten per request.
	if _, err := db.Exec(`DELETE FROM responses WHERE request_id = ? AND id NOT IN (
		SELECT id FROM responses WHERE request_id = ? ORDER BY id DESC LIMIT 10)`,
		resp.RequestID, resp.RequestID); err != nil {
		return models.HttpResponse{}, fmt.Errorf("trimming response history: %w", err)
	}
	return resp, nil
}

// UpdateResponse rewrites a stored response row (the in-flight duration
// updates and the final body of a single send).
type UpdateResponse struct{}

func (UpdateResponse) Info() commands.Info {
	return commands.Info{
		FunctionID: "response.update",
		Label:      "Update Response",
		Columns:    []string{"request_id", "headers", "status_code", "body", "created_at", "duration_ms"},
	}
}

func (UpdateResponse) Run(db store.Session, input json.RawMessage) (any, error) {
	var resp models.HttpResponse
	if err := json.Unmarshal(input, &resp); err != nil {
		return nil, err
	}
	if resp.ID == 0 {
		return nil, fmt.Errorf("response id is required")
	}
	if err := db.Update("responses", resp.ID, map[string]any{
		"request_id":  resp.RequestID,
		"headers":     resp.Headers,
		"status_code": resp.StatusCode,
		"body":        resp.Body,
		"created_at":  resp.CreatedAt,
		"duration_ms": resp.DurationMs,
	}); err != nil {
		return nil, err
	}
	return resp, nil
}

// RecordRequestResponse notes a send's outcome on the request row: its
// latest status code and newest response id. The send flow's owned write —
// user-edit commands never touch these columns.
type RecordRequestResponse struct{}

func (RecordRequestResponse) Info() commands.Info {
	return commands.Info{
		FunctionID: "request.record_response",
		Label:      "Record Response",
		Columns:    []string{"status_code", "response_id"},
	}
}

func (RecordRequestResponse) Run(db store.Session, input json.RawMessage) (any, error) {
	var payload struct {
		RequestID  int64 `json:"request_id"`
		StatusCode int   `json:"status_code"`
		ResponseID int64 `json:"response_id"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, err
	}
	if err := db.Update("http_requests", payload.RequestID, map[string]any{
		"status_code": payload.StatusCode,
		"response_id": payload.ResponseID,
	}); err != nil {
		return nil, err
	}
	return nil, nil
}

// SaveResponseToFile writes a response's body to a file. It changes
// nothing in the database, so it runs lock-free like other ReadOnly
// commands; the file write is an external effect, never undone.
type SaveResponseToFile struct{}

func (SaveResponseToFile) Info() commands.Info {
	return commands.Info{FunctionID: "response.save_to_file", Label: "Save Response To File", ReadOnly: true}
}

func (SaveResponseToFile) Run(db store.Session, input json.RawMessage) (any, error) {
	var payload struct {
		ResponseID int64  `json:"response_id"`
		FilePath   string `json:"file_path"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, err
	}

	var body string
	err := db.QueryRow(
		`SELECT COALESCE(body, '') FROM responses WHERE id = ?`, payload.ResponseID,
	).Scan(&body)
	if err != nil {
		return nil, fmt.Errorf("loading the response: %w", err)
	}
	if err := os.WriteFile(payload.FilePath, []byte(body), 0644); err != nil {
		return nil, fmt.Errorf("writing the file: %w", err)
	}
	return nil, nil
}

// RecordRequestUsage marks requests as most-recently used. Telemetry:
// never undone, never restored by an undo.
type RecordRequestUsage struct{}

func (RecordRequestUsage) Info() commands.Info {
	return commands.Info{FunctionID: "request.record_usage", Label: "Record Usage"}
}

func (RecordRequestUsage) Run(db store.Session, input json.RawMessage) (any, error) {
	var payload struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil, err
	}
	if len(payload.IDs) == 0 {
		return nil, nil
	}

	usedAt := time.Now().UTC().UnixNano()
	for _, id := range payload.IDs {
		if _, err := db.Exec(`
			INSERT INTO request_usage (request_id, last_used_at) VALUES (?, ?)
			ON CONFLICT(request_id) DO UPDATE SET last_used_at = excluded.last_used_at`,
			id, usedAt); err != nil {
			return nil, fmt.Errorf("recording usage for request %d: %w", id, err)
		}
	}
	return nil, nil
}
