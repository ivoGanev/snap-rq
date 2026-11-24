package mcp

import (
	"errors"
	"strings"
	"testing"
)

func TestBulkEditOnlyChangesNameAndColor(t *testing.T) {
	for _, field := range []string{"url", "headers", "body", "method", "body_type", "body_form", "nonsense"} {
		f := &fakeRunner{}
		if _, err := bulkEdit(newFakeServer(f), bulkEditInput{IDs: []int64{1}, Field: field, Value: "x"}); err == nil {
			t.Errorf("bulk_edit_requests accepted %q", field)
		}
		if len(f.calls) != 0 {
			t.Errorf("%q reached the runner", field)
		}
	}
	for _, field := range []string{"name", "color"} {
		f := &fakeRunner{}
		if _, err := bulkEdit(newFakeServer(f), bulkEditInput{IDs: []int64{1}, Field: field, Value: "x"}); err != nil {
			t.Errorf("%s: %v", field, err)
		}
		if len(f.calls) != 1 || f.calls[0].command != "request.bulk_set" {
			t.Errorf("%s: calls = %v", field, f.calls)
		}
	}
}

func TestEditRequestContentOnlyChangesWhatARequestSends(t *testing.T) {
	for _, field := range []string{"name", "color", "headers", "nonsense"} {
		f := &fakeRunner{}
		in := contentEditInput{Changes: []contentChange{{ID: 1, Field: "url", Value: "http://x"}, {ID: 2, Field: field, Value: "x"}}}
		if _, err := editRequestContent(newFakeServer(f), in); err == nil {
			t.Errorf("edit_request_content accepted %q", field)
		}
		if len(f.calls) != 0 {
			t.Errorf("%q reached the runner", field)
		}
	}
	f := &fakeRunner{}
	in := contentEditInput{Changes: []contentChange{
		{ID: 1, Field: "url", Value: "http://x"},
		{ID: 2, Field: "request_headers", Value: "A: 1"},
	}}
	if _, err := editRequestContent(newFakeServer(f), in); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 || f.calls[0].command != "request.edit_many" {
		t.Fatalf("calls = %v, want one request.edit_many", f.calls)
	}
}

func TestHeadersPointsToRequestHeaders(t *testing.T) {
	in := contentEditInput{Changes: []contentChange{{ID: 1, Field: "headers", Value: "A: 1"}}}
	_, err := editRequestContent(newFakeServer(&fakeRunner{}), in)
	if err == nil || !strings.Contains(err.Error(), "request_headers") {
		t.Fatalf("err = %v", err)
	}
}

func TestAFailedContentCallSaysNothingChanged(t *testing.T) {
	f := &fakeRunner{failOn: map[string]error{
		"request.edit_many":   errors.New("change 2 (request 38): body must be a string"),
		"request.create_many": errors.New("request 2 of 2: collection 9 doesn't exist"),
	}}
	s := newFakeServer(f)
	_, err := editRequestContent(s, contentEditInput{Changes: []contentChange{{ID: 31, Field: "url", Value: "x"}}})
	if err == nil || !strings.HasPrefix(err.Error(), "nothing was changed: ") || !strings.Contains(err.Error(), "request 38") {
		t.Errorf("edit err = %v", err)
	}
	_, err = createRequests(s, createRequestsInput{Requests: []newRequest{{CollectionID: 1, Name: "a"}}})
	if err == nil || !strings.HasPrefix(err.Error(), "no request was created: ") {
		t.Errorf("create err = %v", err)
	}
}

func TestCreateRequestsForwardsContentAndDefaultsTheMethod(t *testing.T) {
	f := &fakeRunner{}
	in := createRequestsInput{Requests: []newRequest{
		{CollectionID: 3, Name: "a", URL: "http://x", Body: `{"priority": 5}`, RequestHeaders: "A: 1"},
		{CollectionID: 3, Name: "b", Method: " post "},
	}}
	if _, err := createRequests(newFakeServer(f), in); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 || f.calls[0].command != "request.create_many" {
		t.Fatalf("calls = %v", f.calls)
	}
	sent := f.calls[0].input["requests"].([]any)
	first, second := sent[0].(map[string]any), sent[1].(map[string]any)
	if first["method"] != "GET" || second["method"] != "POST" {
		t.Errorf("methods = %v, %v", first["method"], second["method"])
	}
	if first["body"] != `{"priority": 5}` || first["request_headers"] != "A: 1" || first["url"] != "http://x" {
		t.Errorf("content not forwarded: %v", first)
	}

	if _, err := createRequests(newFakeServer(&fakeRunner{}), createRequestsInput{Requests: []newRequest{{Name: "no collection"}}}); err == nil {
		t.Error("a request without a collection was accepted")
	}
}

func TestCreateRequestHasNoContentFields(t *testing.T) {
	f := &fakeRunner{}
	if _, err := createRequest(newFakeServer(f), createRequestInput{CollectionID: 3, Name: "a"}); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"url", "body", "request_headers", "body_type"} {
		if _, has := f.calls[0].input[k]; has {
			t.Errorf("create_request forwards %q", k)
		}
	}
}
