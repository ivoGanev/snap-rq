package models

import "regexp"

// VariableNameChars is the allowlist for an environment variable's name. The
// frontend gets it through VariableNameRule; testdata/variable-names.json pins it.
const VariableNameChars = `A-Za-z0-9_.-`

var variableName = regexp.MustCompile(`^[` + VariableNameChars + `]+$`)

// VariablePlaceholder matches {{name}} with optional padding; group 1 is the name.
var VariablePlaceholder = regexp.MustCompile(`\{\{\s*([` + VariableNameChars + `]+)\s*\}\}`)

// ValidVariableName reports whether name can be used as {{name}}.
func ValidVariableName(name string) bool {
	return variableName.MatchString(name)
}

// VariableNameError explains the allowlist to someone who typed a name outside it.
const VariableNameError = "Use only letters, digits, _, - and ."

// VariableNameRule is what the frontend builds its {{name}} matching from.
type VariableNameRule struct {
	Chars   string `json:"chars"`
	Message string `json:"message"`
}
