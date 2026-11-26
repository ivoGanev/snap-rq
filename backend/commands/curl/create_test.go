package curl

import (
	"strings"
	"testing"

	"snap-rq/backend/models"
	"snap-rq/backend/protocol/httpreq"
)

func TestFromOutgoingWritesTheRequestAsItGoesOut(t *testing.T) {
	out, err := httpreq.Build(models.HttpRequest{
		Method: "post", URL: "api.example.com/items", BodyType: "json", Body: `{"a":1}`,
		RequestHeaders: "X-Zeta: z\nAuthorization: Bearer t",
	})
	if err != nil {
		t.Fatal(err)
	}
	curl := FromOutgoing(out)

	want := `curl -X POST 'http://api.example.com/items' -H 'Authorization: Bearer t' ` +
		`-H 'Content-Type: application/json' -H 'X-Zeta: z' --data-raw '{"a":1}'`
	if curl != want {
		t.Errorf("curl =\n%s\nwant\n%s", curl, want)
	}
}

func TestFromOutgoingWritesFormFields(t *testing.T) {
	out, err := httpreq.Build(models.HttpRequest{
		Method: "POST", URL: "https://x", BodyType: "form-data",
		BodyForm: `[{"key":"title","type":"text","value":"Hi","enabled":true},
			{"key":"doc","type":"file","file_path":"/files/a.txt","enabled":true},
			{"key":"off","type":"text","value":"no","enabled":false}]`,
	})
	if err != nil {
		t.Fatal(err)
	}
	curl := FromOutgoing(out)
	for _, want := range []string{"-F 'title=Hi'", "-F 'doc=@/files/a.txt'"} {
		if !strings.Contains(curl, want) {
			t.Errorf("curl lacks %s:\n%s", want, curl)
		}
	}
	if strings.Contains(curl, "off=") || strings.Contains(curl, "--data-raw") {
		t.Errorf("curl has a disabled field or a raw body:\n%s", curl)
	}
}
