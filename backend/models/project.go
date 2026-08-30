package models

// Project represents a project that belongs to a profile and holds collections.
type Project struct {
	ID        int64  `json:"id"`
	ProfileID int64  `json:"profile_id"`
	Name      string `json:"name"`
}
