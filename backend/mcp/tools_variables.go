package mcp

import "encoding/json"

// Variables: the AI may see which variables exist, never their values. Values
// are dropped here, in Go, so no setting or client can bring them back.

type environmentInput struct {
	EnvironmentID int64 `json:"environment_id" jsonschema:"id of the environment"`
}

// VariableName is all a client learns about a variable.
type VariableName struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func init() {
	bindings = append(bindings,
		binding{"list_environments", addTool[projectInput]},
		binding{"list_variables", customTool(listVariables)},
	)
}

// listVariables returns variable names only. It decodes into VariableName, so
// any other field the command returns (value included) cannot pass through.
func listVariables(s *Server, in environmentInput) (any, error) {
	out, err := s.run("variable.list", in)
	if err != nil {
		return nil, err
	}
	return variableNames(out)
}

func variableNames(out any) ([]VariableName, error) {
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ID  int64  `json:"id"`
		Key string `json:"key"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	names := make([]VariableName, 0, len(rows))
	for _, r := range rows {
		names = append(names, VariableName{ID: r.ID, Name: r.Key})
	}
	return names, nil
}
