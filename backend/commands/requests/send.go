package requests

import (
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"snap-rq/backend/commands"
	"snap-rq/backend/models"
)

// send.go — execute a request against the outside world. Read-only: it
// writes nothing to the database (the caller persists the outcome via
// response.save / response.update), so it runs without the writer lock —
// a slow send never blocks a rename. Never undoable.

type Send struct{ tx *sql.Tx }

func NewSend(tx *sql.Tx) commands.Command { return &Send{tx: tx} }

func (Send) Info() commands.Info {
	return commands.Info{FunctionID: "request.send", Label: "Send Request", ReadOnly: true}
}

func (c Send) Run(input json.RawMessage) (any, error) {
	var req models.HttpRequest
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	if req.ID == 0 {
		return nil, fmt.Errorf("request id is required")
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

	client, err := c.httpClient()
	if err != nil {
		return nil, err
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

// httpClient builds the client from the user's settings; certificate
// validation is skipped only when the user explicitly disabled it.
func (c Send) httpClient() (*http.Client, error) {
	timeoutSeconds, validateSSL := 30, true
	var raw string
	if err := c.tx.QueryRow(
		`SELECT value FROM app_settings WHERE key = 'request_timeout_seconds'`,
	).Scan(&raw); err == nil {
		_ = json.Unmarshal([]byte(raw), &timeoutSeconds)
	}
	if err := c.tx.QueryRow(
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
