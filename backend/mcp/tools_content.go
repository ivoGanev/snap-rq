package mcp

import "fmt"

// Content tools change what a request sends. They are a separate class, off
// by default: a client that can rewrite URLs and headers could point your
// saved credentials at another host.

type contentEditInput struct {
	IDs   []int64 `json:"ids" jsonschema:"ids of the requests to change"`
	Field string  `json:"field" jsonschema:"one of url, method, headers, body, body_type, body_form"`
	Value any     `json:"value" jsonschema:"the new value"`
}

var contentFields = map[string]bool{
	"url": true, "method": true, "headers": true, "body": true, "body_type": true, "body_form": true,
}

func init() {
	bindings = append(bindings, binding{"edit_request_content", customTool(editRequestContent)})
}

func editRequestContent(s *Server, in contentEditInput) (any, error) {
	if !contentFields[in.Field] {
		return nil, fmt.Errorf("field must be one of url, method, headers, body, body_type, body_form, not %q; use bulk_edit_requests for name and color", in.Field)
	}
	return s.run("request.bulk_set", in)
}
