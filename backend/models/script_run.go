package models

import "encoding/json"

// ScriptRun is what one script did in one phase of a send: its console
// output, test results and error. A response keeps the runs of its send
// (HttpResponse.ScriptLog).
type ScriptRun struct {
	ScriptID   int64         `json:"script_id"`
	Script     string        `json:"script"`
	Phase      string        `json:"phase"` // "beforeRequest" | "afterResponse"
	DurationMs int64         `json:"duration_ms"`
	Console    []ConsoleLine `json:"console"`
	Tests      []TestResult  `json:"tests"`
	Error      string        `json:"error,omitempty"`
}

// ConsoleLine is one console.log (or info/warn/error) call.
type ConsoleLine struct {
	Level string `json:"level"` // "log" | "info" | "warn" | "error"
	Text  string `json:"text"`
}

// TestResult is one ctx.test(...).
type TestResult struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Error  string `json:"error,omitempty"`
}

// ParseScriptLog reads a response's stored script log ('' when no script ran).
func ParseScriptLog(raw string) []ScriptRun {
	if raw == "" {
		return nil
	}
	var log []ScriptRun
	if err := json.Unmarshal([]byte(raw), &log); err != nil {
		return nil
	}
	return log
}

// FormatScriptLog is the stored form of a script log ('' when no script ran).
func FormatScriptLog(log []ScriptRun) string {
	if len(log) == 0 {
		return ""
	}
	raw, err := json.Marshal(log)
	if err != nil {
		return ""
	}
	return string(raw)
}
