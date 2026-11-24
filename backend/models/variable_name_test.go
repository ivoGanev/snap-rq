package models

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type variableNameCases struct {
	Chars   string   `json:"chars"`
	Valid   []string `json:"valid"`
	Invalid []string `json:"invalid"`
}

func loadVariableNameCases(t *testing.T) variableNameCases {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "variable-names.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases variableNameCases
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	return cases
}

func TestValidVariableName(t *testing.T) {
	cases := loadVariableNameCases(t)
	// The frontend spec builds its rule from the fixture's chars.
	if cases.Chars != VariableNameChars {
		t.Fatalf("testdata/variable-names.json has chars %q, want %q", cases.Chars, VariableNameChars)
	}
	for _, name := range cases.Valid {
		if !ValidVariableName(name) {
			t.Errorf("%q should be a valid name", name)
		}
	}
	for _, name := range cases.Invalid {
		if ValidVariableName(name) {
			t.Errorf("%q should be an invalid name", name)
		}
	}
}

// Every name that can be saved must also be found as {{name}}, or it never resolves.
func TestPlaceholderMatchesExactlyTheValidNames(t *testing.T) {
	cases := loadVariableNameCases(t)
	for _, name := range cases.Valid {
		for _, text := range []string{"{{" + name + "}}", "{{ " + name + " }}"} {
			m := VariablePlaceholder.FindStringSubmatch(text)
			if m == nil || m[0] != text || m[1] != name {
				t.Errorf("%q: got %q, want the whole placeholder with name %q", text, m, name)
			}
		}
	}
	for _, name := range cases.Invalid {
		if m := VariablePlaceholder.FindStringSubmatch("{{" + name + "}}"); m != nil && m[1] == name {
			t.Errorf("{{%s}} matched as a variable, but %q can't be saved", name, name)
		}
	}
}
