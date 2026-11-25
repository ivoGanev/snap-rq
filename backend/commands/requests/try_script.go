package requests

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"snap-rq/backend/commands"
	"snap-rq/backend/environment"
	"snap-rq/backend/models"
	"snap-rq/backend/scripting"
)

// try_script.go — try one script's hook against a request, without sending
// anything: beforeRequest shows the request it would produce (variables
// filled in, secret values masked), afterResponse runs against the request's
// last saved response. It lives next to send.go because it is send's
// pipeline minus the network. Only this script runs: values earlier scripts
// would put in ctx.vars aren't there.

type TryScript struct{ tx *sql.Tx }

func NewTryScript(tx *sql.Tx) commands.Command { return &TryScript{tx: tx} }

func (TryScript) Info() commands.Info {
	return commands.Info{FunctionID: "script.try", Label: "Try Script", ReadOnly: true}
}

type tryScriptInput struct {
	// Content is the code to try: the editor's text, saved or not.
	Content       string `json:"content"`
	ScriptName    string `json:"script_name"`
	RequestID     int64  `json:"request_id"`
	Phase         string `json:"phase"` // "beforeRequest" | "afterResponse"
	EnvironmentID int64  `json:"environment_id"`
}

// TryResult is what trying a hook did. Problems means the code doesn't
// compile and nothing ran; Note explains when nothing could run (no hook,
// no saved response).
type TryResult struct {
	Problems []scripting.Problem `json:"problems"`
	Note     string              `json:"note,omitempty"`
	// beforeRequest: the request as it would be sent, or why not.
	Request *TriedRequest `json:"request,omitempty"`
	Stopped string        `json:"stopped,omitempty"`
	// afterResponse: the response it ran against.
	Response *TriedResponse     `json:"response,omitempty"`
	Log      []models.ScriptRun `json:"log"`
}

type TriedRequest struct {
	Method  string `json:"method"`
	URL     string `json:"url"`
	Headers string `json:"headers"`
	Body    string `json:"body"`
}

type TriedResponse struct {
	ID         int64  `json:"id"`
	StatusCode int    `json:"status_code"`
	CreatedAt  string `json:"created_at"`
}

func (c TryScript) Run(input json.RawMessage) (any, error) {
	var in tryScriptInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	result := TryResult{Problems: scripting.Check(in.Content), Log: []models.ScriptRun{}}
	if len(result.Problems) > 0 {
		return result, nil
	}
	result.Problems = []scripting.Problem{} // compiles: an empty list, not null
	req, err := queryRequest(c.tx, in.RequestID)
	if err != nil {
		return nil, err
	}
	env, err := environment.Load(c.tx, in.EnvironmentID)
	if err != nil {
		return nil, err
	}
	name := in.ScriptName
	if name == "" {
		name = "script"
	}
	session := scripting.NewSession(context.Background(), []scripting.Script{{Name: name, Source: in.Content}}, env)

	switch in.Phase {
	case scripting.PhaseBefore:
		c.tryBefore(&result, session, env, req)
	case scripting.PhaseAfter:
		if err := c.tryAfter(&result, session, req); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown phase %q", in.Phase)
	}
	result.Log = session.Log(func(text string) string { return hideSecrets(text, env.Revealed()) })
	if result.Log == nil {
		result.Log = []models.ScriptRun{}
	}
	return result, nil
}

// tryBefore fills in variables, runs beforeRequest and fills in the
// placeholders it wrote, as sending does; secret values are masked for
// display.
func (c TryScript) tryBefore(result *TryResult, session *scripting.Session, env *environment.Variables, req models.HttpRequest) {
	mask := func(text string) string { return hideSecrets(text, env.Revealed()) }
	// A locked secret stays a {{placeholder}} here: trying shows the request as it stands.
	req, _ = env.FillRequest(req)

	scripted := scripting.Request{
		Method: req.Method, URL: req.URL, Body: req.Body,
		Headers: scripting.ParseHeaders(req.RequestHeaders),
	}
	stopped, err := session.BeforeRequest(&scripted)
	if err != nil {
		result.Stopped = mask(err.Error())
		return
	}
	if stopped != "" {
		result.Stopped = mask(stopped)
		return
	}
	if !session.Defines(scripting.PhaseBefore) {
		result.Note = "This script has no beforeRequest, so the request is sent as it is."
	}
	req.Method, req.URL, req.Body = scripted.Method, scripted.URL, scripted.Body
	if scripted.Headers.Changed() {
		req.RequestHeaders = scripted.Headers.String()
	}
	req, _ = env.FillRequest(req)

	body := req.Body
	if normalizeBodyType(req.BodyType) == "form-data" {
		body = "(form data, filled in when sent)"
	}
	result.Request = &TriedRequest{
		Method:  req.Method,
		URL:     mask(ensureScheme(req.URL)),
		Headers: mask(req.RequestHeaders),
		Body:    mask(body),
	}
}

// tryAfter runs afterResponse against the request's newest saved response.
func (c TryScript) tryAfter(result *TryResult, session *scripting.Session, req models.HttpRequest) error {
	var res models.HttpResponse
	err := c.tx.QueryRow(`
		SELECT id, COALESCE(headers, ''), status_code, COALESCE(body, ''), created_at, duration_ms
		FROM responses WHERE request_id = ? ORDER BY id DESC LIMIT 1`, req.ID,
	).Scan(&res.ID, &res.Headers, &res.StatusCode, &res.Body, &res.CreatedAt, &res.DurationMs)
	if errors.Is(err, sql.ErrNoRows) {
		result.Note = "This request has no response yet: send it once, then try afterResponse against it."
		return nil
	}
	if err != nil {
		return fmt.Errorf("loading the last response: %w", err)
	}
	result.Response = &TriedResponse{ID: res.ID, StatusCode: res.StatusCode, CreatedAt: res.CreatedAt}
	if res.StatusCode == 0 {
		result.Note = "The last send got no response (it failed), so afterResponse wouldn't run. Send it again once it works."
		return nil
	}

	if err := session.Load(); err != nil {
		result.Stopped = err.Error()
		return nil
	}
	if !session.Defines(scripting.PhaseAfter) {
		result.Note = "This script has no afterResponse."
		return nil
	}
	session.AfterResponse(&scripting.Response{
		Status: res.StatusCode, Headers: scripting.ParseHeaders(res.Headers),
		Body: res.Body, DurationMs: res.DurationMs,
	})
	return nil
}
