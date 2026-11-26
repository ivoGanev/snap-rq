package requests

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"snap-rq/backend/commands"
	"snap-rq/backend/commands/responses"
	"snap-rq/backend/cookies"
	"snap-rq/backend/models"
	"snap-rq/backend/protocol/httpreq"
	"snap-rq/backend/repository"
	"snap-rq/backend/scripting"
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

var errCancelled = errors.New("request cancelled")

func (c Send) Run(input json.RawMessage, obs commands.Observer) {
	var in struct {
		Request models.HttpRequest `json:"request"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		obs.Finish(nil, err)
		return
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
	env, err := repository.NewEnvironments(c.db).Current(req.ProjectID,
		repository.Load{Variables: true, Secrets: repository.Decrypt})
	if err != nil {
		obs.Finish(nil, err)
		return
	}
	b := httpreq.New(env)
	if req, err = resolve(b, req); err != nil {
		obs.Finish(nil, err)
		return
	}

	scripts, err := c.scriptsFor(req.ID)
	if err != nil {
		obs.Finish(nil, err)
		return
	}
	mask := func(text string) string { return hideSecrets(text, env.SecretValues()) }
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

		if req, err = resolve(b, req); err != nil {
			obs.Finish(nil, err)
			return
		}
	}

	sent := time.Now()
	result := c.doHTTP(ctx, req)
	result.DurationMs = time.Since(sent).Milliseconds()
	result.URL = mask(result.URL)
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

// resolve fills in the request's variables. A secret it uses that the vault
// can't open is an error naming it, rather than sending "{{KEY}}".
func resolve(b httpreq.Builder, req models.HttpRequest) (models.HttpRequest, error) {
	resolved, locked := b.Resolve(req)
	if len(locked) > 0 {
		return req, fmt.Errorf("the secret variable %s is locked: restore your recovery key or enter its value again", locked[0])
	}
	return resolved, nil
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

// secretMask is what a secret value is shown as. Not "{{KEY}}": that would
// look like a variable that wasn't filled in.
const secretMask = "•••••"

// hideSecrets replaces secret values in text meant for display (a failed
// request's message, a script's log).
func hideSecrets(text string, values []string) string {
	for _, value := range values {
		if value != "" {
			text = strings.ReplaceAll(text, value, secretMask)
		}
	}
	return text
}

// doHTTP runs the request and returns a response; network and build errors
// become a status-0 response body instead of a Go error.
func (c Send) doHTTP(ctx context.Context, req models.HttpRequest) models.HttpResponse {
	built, err := httpreq.Build(req)
	fail := func(body string) models.HttpResponse {
		return models.HttpResponse{RequestID: req.ID, StatusCode: 0, Body: body, Method: built.Method, URL: built.URL}
	}
	if err != nil {
		return fail(err.Error())
	}
	httpReq, err := built.HTTPRequest(ctx)
	if err != nil {
		return fail(err.Error())
	}

	client, err := c.httpClient(req.ProjectID)
	if err != nil {
		return fail(fmt.Sprintf("building client: %v", err))
	}
	httpResp, err := client.Do(httpReq)
	if err != nil {
		return fail(httpreq.WithDetails(explainSendError(err, httpReq, client.Timeout), fmt.Sprintf("request failed: %v", err)))
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
		Method:     httpResp.Request.Method,
		URL:        httpResp.Request.URL.String(),
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
