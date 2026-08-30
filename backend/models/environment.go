package models

import "time"

// Environment represents a set of variables tied to a project (e.g. Production, Staging).
type Environment struct {
	ID        int64     `json:"id"`
	ProjectID int64     `json:"project_id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}
