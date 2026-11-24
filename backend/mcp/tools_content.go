package mcp

import (
	"errors"
	"fmt"
	"strings"
)

// Content tools change what a request sends. They are a separate class, and
// every call asks the user first: a client that can write URLs and headers
// could point your saved credentials at another host.

type contentChange struct {
	ID    int64  `json:"id" jsonschema:"id of the request to change"`
	Field string `json:"field" jsonschema:"one of url, method, request_headers, body, body_type, body_form (the names get_request returns)"`
	Value string `json:"value" jsonschema:"the new value, always a string: send a JSON body as its text"`
}

type contentEditInput struct {
	Changes []contentChange `json:"changes" jsonschema:"the changes, applied together: if one fails, none is made"`
}

type newRequest struct {
	CollectionID   int64  `json:"collection_id" jsonschema:"collection the request is added to, at its end"`
	Name           string `json:"name" jsonschema:"request name"`
	Method         string `json:"method,omitempty" jsonschema:"HTTP method, default GET"`
	URL            string `json:"url,omitempty" jsonschema:"address; may use {{variables}}"`
	RequestHeaders string `json:"request_headers,omitempty" jsonschema:"one 'Name: value' per line"`
	Body           string `json:"body,omitempty" jsonschema:"the body as text, including a JSON body"`
	BodyType       string `json:"body_type,omitempty" jsonschema:"json (default), text or form-data"`
	BodyForm       string `json:"body_form,omitempty" jsonschema:"form-data fields, as get_request returns them"`
}

type createRequestsInput struct {
	Requests []newRequest `json:"requests" jsonschema:"the requests, created in this order; if one fails, none is created"`
}

var contentFields = map[string]bool{
	"url": true, "method": true, "request_headers": true, "body": true, "body_type": true, "body_form": true,
}

const contentFieldNames = "url, method, request_headers, body, body_type, body_form"

func init() {
	bindings = append(bindings,
		binding{"edit_request_content", customTool(editRequestContent)},
		binding{"create_requests", customTool(createRequests)},
	)
}

func editRequestContent(s *Server, in contentEditInput) (any, error) {
	if len(in.Changes) == 0 {
		return nil, errors.New("changes must not be empty")
	}
	for i, c := range in.Changes {
		if contentFields[c.Field] {
			continue
		}
		switch c.Field {
		case "headers":
			return nil, fmt.Errorf("change %d: the field is request_headers, as get_request names it", i+1)
		case "name", "color":
			return nil, fmt.Errorf("change %d: %s is changed with bulk_edit_requests", i+1, c.Field)
		}
		return nil, fmt.Errorf("change %d: field must be one of %s, not %q", i+1, contentFieldNames, c.Field)
	}
	out, err := s.run("request.edit_many", in)
	if err != nil {
		return nil, fmt.Errorf("nothing was changed: %w", err)
	}
	return out, nil
}

func createRequests(s *Server, in createRequestsInput) (any, error) {
	if len(in.Requests) == 0 {
		return nil, errors.New("requests must not be empty")
	}
	for i := range in.Requests {
		r := &in.Requests[i]
		if r.CollectionID == 0 {
			return nil, fmt.Errorf("request %d: collection_id is required", i+1)
		}
		r.Method = strings.ToUpper(strings.TrimSpace(r.Method))
		if r.Method == "" {
			r.Method = "GET"
		}
	}
	out, err := s.run("request.create_many", in)
	if err != nil {
		return nil, fmt.Errorf("no request was created: %w", err)
	}
	return out, nil
}
