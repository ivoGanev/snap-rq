package mcp

import "testing"

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
	for _, field := range []string{"name", "color", "nonsense"} {
		f := &fakeRunner{}
		if _, err := editRequestContent(newFakeServer(f), contentEditInput{IDs: []int64{1}, Field: field, Value: "x"}); err == nil {
			t.Errorf("edit_request_content accepted %q", field)
		}
	}
	f := &fakeRunner{}
	if _, err := editRequestContent(newFakeServer(f), contentEditInput{IDs: []int64{1}, Field: "url", Value: "http://x"}); err != nil {
		t.Fatal(err)
	}
	if f.calls[0].command != "request.bulk_set" || f.calls[0].input["field"] != "url" {
		t.Fatalf("calls = %v", f.calls)
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
