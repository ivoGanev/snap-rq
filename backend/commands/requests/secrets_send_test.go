package requests_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"snap-rq/backend/models"
)

const secret = "sk-live-7f3a9c"

func TestASecretReachesTheServerEverywhereItIsUsed(t *testing.T) {
	h := newHarness(t)
	env := h.environment(nil, map[string]string{"TOKEN": secret})
	id := h.create(models.HttpRequest{
		Method: "POST", URL: h.url("/x?key={{TOKEN}}"), BodyType: "text", Body: "token={{ TOKEN }}",
		RequestHeaders: "Authorization: Bearer {{TOKEN}}",
	})
	form := h.create(models.HttpRequest{
		Method: "POST", URL: h.url("/form"), BodyType: "form-data",
		BodyForm: `[{"key":"token","type":"text","value":"{{TOKEN}}","enabled":true}]`,
	})

	h.mustSend(id, sendOptions{environmentID: env})
	got := h.last()
	if got.Query != "key="+secret || string(got.Body) != "token="+secret || got.Header.Get("Authorization") != "Bearer "+secret {
		t.Errorf("server got query %q, body %q, auth %q", got.Query, got.Body, got.Header.Get("Authorization"))
	}

	h.mustSend(form, sendOptions{environmentID: env})
	if body := string(h.last().Body); !strings.Contains(body, secret) {
		t.Errorf("form body = %q, want the secret in the token field", body)
	}
}

func TestASecretNeverShowsInWhatIsSaved(t *testing.T) {
	h := newHarness(t)
	env := h.environment(map[string]string{"PLAIN": "visible"}, map[string]string{"TOKEN": secret})
	id := h.create(models.HttpRequest{Method: "GET", URL: h.url("/x"), RequestHeaders: "Authorization: {{TOKEN}}"})
	h.attach(id, "leaky", `function beforeRequest(req, ctx) {
  console.log("header", req.headers.get("Authorization"));
  console.log("env", ctx.env.get("TOKEN"));
}
function afterResponse(res, ctx) {
  ctx.test("uses " + ctx.env.get("TOKEN"), () => ctx.expect(ctx.env.get("TOKEN")).toBe("wrong"));
}`)

	res := h.mustSend(id, sendOptions{environmentID: env, plain: map[string]string{"PLAIN": "visible"}})

	for _, saved := range []models.HttpResponse{res, h.history(id)[0]} {
		log, _ := json.Marshal(saved.ScriptLog)
		if strings.Contains(string(log), secret) {
			t.Errorf("the script log shows the secret: %s", log)
		}
		if !strings.Contains(string(log), "•••••") {
			t.Errorf("the script log has no mask where the secret was: %s", log)
		}
	}
	if got := h.last().Header.Get("Authorization"); got != secret {
		t.Errorf("the server got %q, want the real value", got)
	}
}

func TestASecretInAFailedOrStoppedSendIsMasked(t *testing.T) {
	h := newHarness(t)
	env := h.environment(nil, map[string]string{"TOKEN": secret})

	unreachable := h.create(models.HttpRequest{Method: "GET", URL: "http://127.0.0.1:1/?key={{TOKEN}}"})
	res := h.mustSend(unreachable, sendOptions{environmentID: env})
	if res.StatusCode != 0 || strings.Contains(res.Body, secret) {
		t.Errorf("failed send body = %q, want an error without the secret", res.Body)
	}

	stopped := h.create(models.HttpRequest{Method: "GET", URL: h.url("/x")})
	h.attach(stopped, "stop", `function beforeRequest(req, ctx) {
  throw new Error("bad token " + ctx.env.get("TOKEN"));
}`)
	res = h.mustSend(stopped, sendOptions{environmentID: env})
	if strings.Contains(res.Body, secret) || !strings.Contains(res.Body, "•••••") {
		t.Errorf("stopped send body = %q, want the secret masked", res.Body)
	}
}

func TestTheDatabaseFileNeverHoldsASecret(t *testing.T) {
	h := newHarness(t)
	env := h.environment(nil, map[string]string{"TOKEN": secret})
	id := h.create(models.HttpRequest{Method: "GET", URL: h.url("/x"), RequestHeaders: "Authorization: {{TOKEN}}"})
	h.attach(id, "print", `function afterResponse(res, ctx) { console.log(ctx.env.get("TOKEN")); }`)
	h.mustSend(id, sendOptions{environmentID: env})

	var vars []models.EnvironmentVariable
	h.run("variable.list", map[string]any{"environment_id": env}, &vars)
	listed, _ := json.Marshal(vars)
	if strings.Contains(string(listed), secret) {
		t.Errorf("variable.list returns the secret: %s", listed)
	}

	var path string
	if err := h.db.QueryRow(`SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&path); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(path + "*")
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), secret) {
			t.Errorf("%s holds the secret in plain text", filepath.Base(file))
		}
	}
}
