package models

import (
	"strings"
	"time"
)

// Environment represents a set of variables tied to a project (e.g. Production, Staging).
type Environment struct {
	ID        int64     `json:"id"`
	ProjectID int64     `json:"project_id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	// Variables are loaded only when asked for (see backend/repository).
	Variables []EnvironmentVariable `json:"variables,omitempty"`
}

// Value is what {{key}} stands for. A secret the vault couldn't open is
// found but locked, and has no value.
func (e Environment) Value(key string) (value string, found, locked bool) {
	for _, v := range e.Variables {
		if v.Key == key {
			return v.Value, true, v.Locked
		}
	}
	return "", false, false
}

// Get is a usable value: unknown and locked keys count as unset. It is
// ctx.env for scripts.
func (e Environment) Get(key string) (string, bool) {
	value, found, locked := e.Value(key)
	return value, found && !locked
}

// Fill replaces every {{key}} the environment has a value for. Unknown keys
// stay as they are, and so do secrets the vault couldn't open: those are
// returned, for the caller to decide whether that's an error.
func (e Environment) Fill(text string) (string, []string) {
	if !strings.Contains(text, "{{") {
		return text, nil
	}
	var locked []string
	filled := VariablePlaceholder.ReplaceAllStringFunc(text, func(match string) string {
		key := VariablePlaceholder.FindStringSubmatch(match)[1]
		value, found, isLocked := e.Value(key)
		if isLocked {
			locked = append(locked, key)
		}
		if !found || isLocked {
			return match
		}
		return value
	})
	return filled, locked
}

// SecretValues are the decrypted secret values, for masking them in logs
// and messages.
func (e Environment) SecretValues() []string {
	var values []string
	for _, v := range e.Variables {
		if v.Secret && !v.Locked && v.Value != "" {
			values = append(values, v.Value)
		}
	}
	return values
}
