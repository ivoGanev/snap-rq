package models

// HttpResponse represents a saved response for an HTTP request.
type HttpResponse struct {
	ID         int64  `json:"id"`
	RequestID  int64  `json:"request_id"`
	Headers    string `json:"headers"`
	StatusCode int    `json:"status_code"`
	Body       string `json:"body"`
	CreatedAt  string `json:"created_at"`
	DurationMs int64  `json:"duration_ms"`
	// Method and URL are what was sent: scripts and redirects applied, secrets hidden.
	Method string `json:"method"`
	URL    string `json:"url"`
	// ScriptLog is what the request's scripts did in this send, in run order.
	ScriptLog []ScriptRun `json:"script_log"`
}
