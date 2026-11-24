package requests

import "testing"

func TestInterpolateFillsEveryAllowedNameWithPadding(t *testing.T) {
	values := map[string]string{"fleet-management": "fm", "api.v2": "v2", "base_url": "http://x"}
	got := interpolate("{{base_url}}/{{ fleet-management }}/{{api.v2}}/{{missing}}", values)
	if want := "http://x/fm/v2/{{missing}}"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
