package requests_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"snap-rq/backend/models"
	"snap-rq/backend/secrets"
)

// The project's selected environment is what sending, trying a script and
// copying as cURL use; these go through the runner like the app.

func (h *harness) storedEnvironment() int64 {
	h.t.Helper()
	var projects []models.Project
	h.run("project.list", map[string]any{}, &projects)
	for _, p := range projects {
		if p.ID == h.projectID {
			return p.SelectedEnvironmentID
		}
	}
	h.t.Fatal("project not listed")
	return 0
}

func (h *harness) curl(requestID int64) string {
	h.t.Helper()
	var curl string
	h.run("curl.create", map[string]any{"request_id": requestID}, &curl)
	return curl
}

func TestSelectingAnEnvironmentStoresItOnTheProject(t *testing.T) {
	h := newHarness(t)
	env := h.environment(map[string]string{"A": "1"}, nil)

	h.selectEnvironment(env)
	if got := h.storedEnvironment(); got != env {
		t.Errorf("stored %d, want %d", got, env)
	}
	h.selectEnvironment(0)
	if got := h.storedEnvironment(); got != 0 {
		t.Errorf("stored %d after selecting none", got)
	}
}

func TestOnlyAnEnvironmentOfTheProjectCanBeSelected(t *testing.T) {
	h := newHarness(t)
	var other models.Project
	h.run("project.create", map[string]any{"profile_id": 1, "name": "Other"}, &other)
	var foreign models.Environment
	h.run("environment.create", map[string]any{"project_id": other.ID, "name": "theirs"}, &foreign)

	if err := h.runErr("environment.select", map[string]any{"project_id": h.projectID, "environment_id": foreign.ID}); err == nil {
		t.Error("another project's environment was selected")
	}
	if err := h.runErr("environment.select", map[string]any{"project_id": 9999, "environment_id": 0}); err == nil {
		t.Error("an unknown project was accepted")
	}
}

func TestRemovingTheSelectedEnvironmentClearsTheSelection(t *testing.T) {
	h := newHarness(t)

	deleted := h.environment(nil, nil)
	h.selectEnvironment(deleted)
	h.run("environment.delete", map[string]any{"id": deleted}, nil)
	if got := h.storedEnvironment(); got != 0 {
		t.Errorf("deleting it left %d stored", got)
	}

	undone := h.environment(nil, nil)
	h.selectEnvironment(undone)
	h.undo() // its creation; selecting isn't an undo step
	if got := h.storedEnvironment(); got != 0 {
		t.Errorf("undoing its creation left %d stored", got)
	}
}

func TestSendUsesTheProjectsEnvironment(t *testing.T) {
	h := newHarness(t)
	staging := h.environment(map[string]string{"HOST": "staging"}, nil)
	prod := h.environment(map[string]string{"HOST": "prod"}, nil)
	id := h.create(models.HttpRequest{Method: "GET", URL: h.url("/{{HOST}}")})

	h.mustSend(id, sendOptions{environmentID: prod})
	if got := h.last().Path; got != "/prod" {
		t.Errorf("sent to %s, want /prod", got)
	}
	h.mustSend(id, sendOptions{environmentID: staging})
	if got := h.last().Path; got != "/staging" {
		t.Errorf("sent to %s, want /staging", got)
	}
}

func TestSendReadsPlainValuesFromTheDatabase(t *testing.T) {
	h := newHarness(t)
	env := h.environment(map[string]string{"HOST": "before"}, nil)
	var vars []models.EnvironmentVariable
	h.run("variable.list", map[string]any{"environment_id": env}, &vars)
	h.run("variable.update", map[string]any{"id": vars[0].ID, "key": "HOST", "value": "after"}, nil)
	id := h.create(models.HttpRequest{Method: "GET", URL: h.url("/{{HOST}}")})

	h.mustSend(id, sendOptions{environmentID: env})
	if got := h.last().Path; got != "/after" {
		t.Errorf("sent to %s, want the saved value", got)
	}
}

func TestALockedSecretTheRequestUsesStopsTheSend(t *testing.T) {
	h := newHarness(t)
	env := h.environment(map[string]string{"HOST": "x"}, map[string]string{"TOKEN": secret})
	uses := h.create(models.HttpRequest{Method: "GET", URL: h.url("/a"), RequestHeaders: "Authorization: {{TOKEN}}"})
	doesnt := h.create(models.HttpRequest{Method: "GET", URL: h.url("/{{HOST}}")})
	secrets.SetDefault(secrets.NewVault(secrets.MemoryKeyStore())) // the key is gone

	if _, err := h.send(uses, sendOptions{environmentID: env}); err == nil || !strings.Contains(err.Error(), "TOKEN is locked") {
		t.Errorf("error = %v, want one naming the locked variable", err)
	}
	if res, err := h.send(doesnt, sendOptions{environmentID: env}); err != nil || res.StatusCode != 200 || h.last().Path != "/x" {
		t.Errorf("a request without the secret failed: %v (%d)", err, res.StatusCode)
	}
}

func TestEverySecretOfTheEnvironmentIsMaskedInTheLog(t *testing.T) {
	h := newHarness(t)
	env := h.environment(nil, map[string]string{"USED": "used-value-1", "OTHER": "other-value-2"})
	id := h.create(models.HttpRequest{Method: "GET", URL: h.url("/x"), RequestHeaders: "X-Key: {{USED}}"})
	h.attach(id, "print", `function afterResponse(res, ctx) {
  console.log(ctx.env.get("USED"), ctx.env.get("OTHER"));
}`)

	res := h.mustSend(id, sendOptions{environmentID: env})
	log, _ := json.Marshal(res.ScriptLog)
	if strings.Contains(string(log), "used-value-1") || strings.Contains(string(log), "other-value-2") {
		t.Errorf("a secret shows in the log: %s", log)
	}
}

func TestAScriptCanWriteAPlaceholderThatGetsFilled(t *testing.T) {
	h := newHarness(t)
	env := h.environment(map[string]string{"WHO": "Ada"}, nil)
	id := h.create(models.HttpRequest{Method: "GET", URL: h.url("/x")})
	h.attach(id, "sign", `function beforeRequest(req, ctx) { req.headers.set("X-Who", "{{WHO}}"); }`)

	h.mustSend(id, sendOptions{environmentID: env})
	if got := h.last().Header.Get("X-Who"); got != "Ada" {
		t.Errorf("X-Who = %q, want the placeholder the script wrote filled in", got)
	}
}

func TestTryingAScriptUsesTheProjectsEnvironment(t *testing.T) {
	h := newHarness(t)
	env := h.environment(map[string]string{"HOST": "eu"}, nil)
	h.selectEnvironment(env)
	id := h.create(models.HttpRequest{Method: "GET", URL: h.url("/{{HOST}}")})
	bad := h.create(models.HttpRequest{Method: "GET", URL: h.url("/x"), RequestHeaders: "not a header"})
	try := func(requestID int64) map[string]any {
		var result map[string]any
		h.run("script.try", map[string]any{"content": `function beforeRequest(req, ctx) {}`,
			"request_id": requestID, "phase": "beforeRequest"}, &result)
		return result
	}

	if req, _ := try(id)["request"].(map[string]any); req == nil || req["url"] != h.url("/eu") {
		t.Errorf("tried request = %v, want the environment's value", req)
	}
	if stopped, _ := try(bad)["stopped"].(string); !strings.Contains(stopped, "Header line 1 isn't a header") {
		t.Errorf("stopped = %q, want the header-line error", stopped)
	}
}

func TestCopyAsCurlFillsInTheProjectsEnvironment(t *testing.T) {
	h := newHarness(t)
	env := h.environment(map[string]string{"NAME": "Ada"}, map[string]string{"TOKEN": secret})
	h.selectEnvironment(env)
	id := h.create(models.HttpRequest{
		Method: "POST", URL: h.url("/items/{{ NAME }}?key={{TOKEN}}&x={{UNKNOWN}}"),
		RequestHeaders: "Authorization: Bearer {{TOKEN}}", BodyType: "json", Body: `{"name":"{{NAME}}"}`,
	})

	curl := h.curl(id)
	for _, want := range []string{
		h.url("/items/Ada?key=" + secret + "&x={{UNKNOWN}}"), "Authorization: Bearer " + secret, `"name":"Ada"`,
	} {
		if !strings.Contains(curl, want) {
			t.Errorf("curl lacks %q:\n%s", want, curl)
		}
	}
}

func TestCopyAsCurlKeepsALockedSecretAndFilePaths(t *testing.T) {
	h := newHarness(t)
	env := h.environment(map[string]string{"NAME": "Ada"}, map[string]string{"TOKEN": secret})
	h.selectEnvironment(env)
	id := h.create(models.HttpRequest{
		Method: "POST", URL: h.url("/upload?t={{TOKEN}}"), BodyType: "form-data",
		BodyForm: `[{"key":"title","type":"text","value":"Hi {{NAME}}","enabled":true},
			{"key":"doc","type":"file","file_path":"/files/{{NAME}}.txt","enabled":true}]`,
	})
	secrets.SetDefault(secrets.NewVault(secrets.MemoryKeyStore())) // the key is gone

	curl := h.curl(id)
	for _, want := range []string{"t={{TOKEN}}", "title=Hi Ada", "doc=@/files/{{NAME}}.txt"} {
		if !strings.Contains(curl, want) {
			t.Errorf("curl lacks %q:\n%s", want, curl)
		}
	}
}

// What Copy as cURL writes is what Send sends: the same URL, headers and body.
func TestCopyAsCurlMatchesWhatSendSends(t *testing.T) {
	h := newHarness(t)
	env := h.environment(map[string]string{"ID": "42"}, map[string]string{"TOKEN": secret})
	id := h.create(models.HttpRequest{
		Method: "put", URL: strings.TrimPrefix(h.url("/items/{{ID}}?q=1"), "http://"), BodyType: "json",
		Body: `{"id":"{{ID}}"}`, RequestHeaders: "Authorization: Bearer {{TOKEN}}\nX-Trace: t-1",
	})

	h.mustSend(id, sendOptions{environmentID: env})
	sent := h.last()
	curl := h.curl(id)

	if !strings.Contains(curl, "-X "+sent.Method+" ") {
		t.Errorf("method %s not in:\n%s", sent.Method, curl)
	}
	if want := "'" + h.url(sent.Path+"?"+sent.Query) + "'"; !strings.Contains(curl, want) {
		t.Errorf("URL %s not in:\n%s", want, curl)
	}
	ignored := []string{"User-Agent", "Accept-Encoding", "Content-Length"}
	for name, values := range sent.Header {
		if slices.Contains(ignored, http.CanonicalHeaderKey(name)) {
			continue
		}
		for _, value := range values {
			if want := "-H '" + name + ": " + value + "'"; !strings.Contains(curl, want) {
				t.Errorf("header %s not in:\n%s", want, curl)
			}
		}
	}
	if want := "--data-raw '" + string(sent.Body) + "'"; !strings.Contains(curl, want) {
		t.Errorf("body %s not in:\n%s", want, curl)
	}
}
