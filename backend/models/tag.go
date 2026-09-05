package models

// Tag is a free-text label that can be attached to many requests.
type Tag struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// RequestTag links a tag to a specific request.
type RequestTag struct {
	ID        int64 `json:"id"`
	RequestID int64 `json:"request_id"`
	TagID     int64 `json:"tag_id"`
}
