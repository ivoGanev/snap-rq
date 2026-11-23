package requests_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"snap-rq/backend/models"
)

func TestBeforeRequestChangesWhatIsSent(t *testing.T) {
	h := newHarness(t)
	id := h.create(models.HttpRequest{Method: "GET", URL: h.url("/original"), RequestHeaders: "X-Keep: yes\nX-Drop: no"})
	h.attach(id, "rewrite", `function beforeRequest(req, ctx) {
  req.method = "POST";
  req.url = req.url.replace("/original", "/rewritten") + "?by=script";
  req.body = JSON.stringify({ from: "script" });
  req.headers.set("X-Added", "1");
  req.headers.set("Authorization", "Bearer {{TOKEN}}");
  req.headers.remove("X-Drop");
}`)

	h.mustSend(id, sendOptions{plain: map[string]string{"TOKEN": "t-123"}})

	got := h.last()
	if got.Method != "POST" || got.Path != "/rewritten" || got.Query != "by=script" {
		t.Errorf("server got %s %s?%s, want POST /rewritten?by=script", got.Method, got.Path, got.Query)
	}
	if string(got.Body) != `{"from":"script"}` {
		t.Errorf("body = %q", got.Body)
	}
	if got.Header.Get("X-Added") != "1" || got.Header.Get("X-Keep") != "yes" || got.Header.Get("X-Drop") != "" {
		t.Errorf("headers = %v", got.Header)
	}
	if got.Header.Get("Authorization") != "Bearer t-123" {
		t.Errorf("a placeholder the script wrote was not filled in: %q", got.Header.Get("Authorization"))
	}
}

func TestScriptsRunInOrderAndShareVars(t *testing.T) {
	h := newHarness(t)
	id := h.create(models.HttpRequest{Method: "GET", URL: h.url("/x")})
	h.attach(id, "first", `function beforeRequest(req, ctx) { ctx.vars.set("trace", "a"); }`)
	h.attach(id, "second", `function beforeRequest(req, ctx) {
  req.headers.set("X-Trace", ctx.vars.get("trace") + "b");
}`)

	h.mustSend(id, sendOptions{})

	if got := h.last().Header.Get("X-Trace"); got != "ab" {
		t.Errorf("X-Trace = %q, want the second script to see the first's var", got)
	}
}

func TestAfterResponseResultsAreSavedWithTheResponse(t *testing.T) {
	h := newHarness(t)
	id := h.create(models.HttpRequest{Method: "GET", URL: h.url("/x")})
	h.attach(id, "checks", `function afterResponse(res, ctx) {
  console.log("status", res.status);
  console.warn("careful");
  ctx.test("is ok", () => ctx.expect(res.json().ok).toBe(true));
  ctx.test("is teapot", () => ctx.expect(res.status).toBe(418));
}`)

	res := h.mustSend(id, sendOptions{})

	history := h.history(id)
	if len(history) != 1 {
		t.Fatalf("history = %d responses, want 1", len(history))
	}
	for _, saved := range []models.HttpResponse{res, history[0]} {
		if len(saved.ScriptLog) != 1 {
			t.Fatalf("script log = %+v", saved.ScriptLog)
		}
		run := saved.ScriptLog[0]
		if run.Phase != "afterResponse" || len(run.Console) != 2 || run.Console[0].Text != "status 200" ||
			run.Console[1].Level != "warn" {
			t.Errorf("console = %+v", run.Console)
		}
		if len(run.Tests) != 2 || !run.Tests[0].Passed || run.Tests[1].Passed ||
			!strings.Contains(run.Tests[1].Error, "418") {
			t.Errorf("tests = %+v", run.Tests)
		}
	}
}

func TestAnAfterResponseErrorKeepsTheResponseAndTheOtherScripts(t *testing.T) {
	h := newHarness(t)
	id := h.create(models.HttpRequest{Method: "GET", URL: h.url("/x")})
	h.attach(id, "broken", `function afterResponse(res, ctx) { null.boom; }`)
	h.attach(id, "fine", `function afterResponse(res, ctx) { console.log("still ran"); }`)

	res := h.mustSend(id, sendOptions{})

	if res.StatusCode != 200 || len(res.ScriptLog) != 2 {
		t.Fatalf("response = %d with log %+v", res.StatusCode, res.ScriptLog)
	}
	if res.ScriptLog[0].Error == "" || res.ScriptLog[1].Console[0].Text != "still ran" {
		t.Errorf("log = %+v", res.ScriptLog)
	}
}

func TestAScriptThatStopsTheSendKeepsItFromTheServer(t *testing.T) {
	cases := []struct {
		name, source, want string
	}{
		{"throws", `function beforeRequest(req, ctx) { throw new Error("no token"); }`, "no token"},
		{"syntax error", `function beforeRequest(req, ctx) { req.url = ; }`, "SyntaxError"},
		{"top-level error", `missing();`, "missing"},
		{"skips", `function beforeRequest(req, ctx) { ctx.skip("offline"); }`, "offline"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			id := h.create(models.HttpRequest{Method: "GET", URL: h.url("/x")})
			h.attach(id, "guard", c.source)

			res := h.mustSend(id, sendOptions{})

			if n := len(h.requests()); n != 0 {
				t.Errorf("%d requests reached the server", n)
			}
			if res.StatusCode != 0 || !strings.Contains(res.Body, `"guard"`) || !strings.Contains(res.Body, c.want) {
				t.Errorf("response = %d %q, want status 0 naming the script and %q", res.StatusCode, res.Body, c.want)
			}
			if len(h.history(id)) != 1 {
				t.Error("the stopped send is not in the history")
			}
		})
	}
}

func TestCancelStopsARunningScript(t *testing.T) {
	h := newHarness(t)
	id := h.create(models.HttpRequest{Method: "GET", URL: h.url("/x")})
	h.attach(id, "spin", `function beforeRequest(req, ctx) { while (true) {} }`)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	start := time.Now()
	_, err := h.send(id, sendOptions{ctx: ctx})

	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("cancel took %s", elapsed)
	}
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("error = %v, want cancelled", err)
	}
	if len(h.requests()) != 0 || len(h.history(id)) != 0 {
		t.Error("a cancelled send reached the server or the history")
	}
}

func TestADisabledOrEmptyScriptDoesNotRun(t *testing.T) {
	h := newHarness(t)
	id := h.create(models.HttpRequest{Method: "GET", URL: h.url("/x")})
	h.attach(id, "empty", "  \n")
	h.attach(id, "off", `function beforeRequest(req, ctx) { throw new Error("ran"); }`)
	if _, err := h.db.Exec(`UPDATE request_scripts SET enabled = 0
		WHERE script_id = (SELECT id FROM scripts WHERE name = 'off')`); err != nil {
		t.Fatal(err)
	}

	res := h.mustSend(id, sendOptions{})

	if res.StatusCode != 200 || len(res.ScriptLog) != 0 {
		t.Errorf("response = %d, log %+v", res.StatusCode, res.ScriptLog)
	}
}
