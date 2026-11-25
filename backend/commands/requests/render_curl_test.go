package requests_test

import (
	"strings"
	"testing"

	"snap-rq/backend/models"
)

func renderCurl(h *harness, id, environmentID int64, plain map[string]string) string {
	h.t.Helper()
	var req models.HttpRequest
	h.run("request.get", map[string]any{"id": id}, &req)
	vars := []map[string]string{}
	for key, value := range plain {
		vars = append(vars, map[string]string{"key": key, "value": value})
	}
	var curl string
	h.run("request.render_curl", map[string]any{"request": req, "variables": vars, "environment_id": environmentID}, &curl)
	return curl
}

func TestCopyAsCurlFillsInVariablesLikeASend(t *testing.T) {
	h := newHarness(t)
	env := h.environment(map[string]string{"NAME": "Ada"}, map[string]string{"TOKEN": secret})
	id := h.create(models.HttpRequest{
		Method: "POST", URL: h.url("/items/{{ NAME }}?key={{TOKEN}}"), BodyType: "json",
		Body:           `{"name":"{{NAME}}","other":"{{UNKNOWN}}"}`,
		RequestHeaders: "Authorization: Bearer {{TOKEN}}",
	})

	curl := renderCurl(h, id, env, map[string]string{"NAME": "Ada"})

	for _, want := range []string{
		h.url("/items/Ada?key=" + secret), "Authorization: Bearer " + secret, `"name":"Ada"`, `"other":"{{UNKNOWN}}"`,
	} {
		if !strings.Contains(curl, want) {
			t.Errorf("curl lacks %q:\n%s", want, curl)
		}
	}
}

func TestCopyAsCurlFillsInFormTextButNotFilePaths(t *testing.T) {
	h := newHarness(t)
	id := h.create(models.HttpRequest{
		Method: "POST", URL: h.url("/upload"), BodyType: "form-data",
		BodyForm: `[{"key":"title","type":"text","value":"Hi {{NAME}}","enabled":true},
			{"key":"doc","type":"file","file_path":"/files/{{NAME}}.txt","file_name":"doc.txt","enabled":true}]`,
	})

	curl := renderCurl(h, id, 0, map[string]string{"NAME": "Ada"})

	if !strings.Contains(curl, "title=Hi Ada") {
		t.Errorf("the text field isn't filled in:\n%s", curl)
	}
	if !strings.Contains(curl, "/files/{{NAME}}.txt") {
		t.Errorf("the file path changed:\n%s", curl)
	}
}

func TestCopyAsCurlWithoutAnEnvironmentKeepsThePlaceholders(t *testing.T) {
	h := newHarness(t)
	id := h.create(models.HttpRequest{Method: "GET", URL: h.url("/items/{{NAME}}")})

	if curl := renderCurl(h, id, 0, nil); !strings.Contains(curl, h.url("/items/{{NAME}}")) {
		t.Errorf("curl = %s, want the placeholder kept", curl)
	}
}
