package models

import "regexp"

// VariableNameChars is the allowlist for an environment variable's name. The
// frontend keeps a copy (environment-context.ts); testdata/variable-names.json
// holds the cases both sides are tested against.
const VariableNameChars = `A-Za-z0-9_.-`

var variableName = regexp.MustCompile(`^[` + VariableNameChars + `]+$`)

// VariablePlaceholder matches {{name}} with optional padding; group 1 is the name.
var VariablePlaceholder = regexp.MustCompile(`\{\{\s*([` + VariableNameChars + `]+)\s*\}\}`)

// ValidVariableName reports whether name can be used as {{name}}.
func ValidVariableName(name string) bool {
	return variableName.MatchString(name)
}

// VariableNameError explains the allowlist to someone who typed a name outside it.
const VariableNameError = "a variable name can only use letters, digits, _, - and ."
