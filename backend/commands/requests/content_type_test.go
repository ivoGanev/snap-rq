package requests_test

import (
	"testing"

	"snap-rq/backend/models"
)

func TestABodyGetsAContentTypeUnlessOneIsSet(t *testing.T) {
	cases := []struct {
		name string
		req  models.HttpRequest
		want string
	}{
		{"json", models.HttpRequest{BodyType: "json", Body: `{"a":1}`}, "application/json"},
		{"text", models.HttpRequest{BodyType: "text", Body: "hello"}, "text/plain"},
		{"own header", models.HttpRequest{BodyType: "json", Body: "{}", RequestHeaders: "content-type: application/vnd.api+json"}, "application/vnd.api+json"},
		{"no body", models.HttpRequest{BodyType: "json"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			c.req.Method, c.req.URL = "POST", h.url("/x")
			h.mustSend(h.create(c.req), sendOptions{})
			if got := h.last().Header.Get("Content-Type"); got != c.want {
				t.Errorf("Content-Type = %q, want %q", got, c.want)
			}
		})
	}
}
