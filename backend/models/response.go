package models

// HttpResponse represents a saved response for an HTTP request.
type HttpResponse struct {
	ID         int64  `json:"id"`
	RequestID  int64  `json:"request_id"`
	Headers    string `json:"headers"`
	StatusCode int    `json:"status_code"`
	Body       string `json:"body"`
}
