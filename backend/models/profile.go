package models

// Profile represents a user profile that can own multiple projects.
type Profile struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}
