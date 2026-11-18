package mcp

import (
	"strings"
	"testing"
)

func TestSummarizeShowsCountsAndShortValuesNotContent(t *testing.T) {
	got := summarize(map[string]any{
		"ids":           []int64{1, 2, 3},
		"collection_id": 12,
		"field":         "headers",
		"value":         "Authorization: Bearer sk-live-12345",
	})
	for _, want := range []string{"ids×3", "collection_id=12", "field=headers"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q lacks %q", got, want)
		}
	}
	if strings.Contains(got, "sk-live") || strings.Contains(got, "value") {
		t.Errorf("summary leaks content: %q", got)
	}
}

func TestMaskArgsHidesSecretsEvenWhenOutputMaskingIsOff(t *testing.T) {
	in := map[string]any{
		"field": "headers",
		"value": "Authorization: Bearer sk-live-12345\nAccept: */*",
		"url":   "http://h/?api_key=abcdef",
	}
	for _, m := range []*Masker{nil, NewMasker(nil)} {
		got := maskArgs(m, in)
		if strings.Contains(got, "sk-live") || strings.Contains(got, "abcdef") {
			t.Fatalf("args leak: %s", got)
		}
		if !strings.Contains(got, "Accept") {
			t.Fatalf("args lost harmless content: %s", got)
		}
	}
}

func TestMaskArgsIsCapped(t *testing.T) {
	got := maskArgs(nil, map[string]any{"value": strings.Repeat("é", 5000)})
	if len(got) > auditMaxArgs+4 {
		t.Fatalf("len = %d", len(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("not truncated: %q", got[len(got)-10:])
	}
}
