package models

// Project represents a project that belongs to a profile and holds collections.
type Project struct {
	ID        int64  `json:"id"`
	ProfileID int64  `json:"profile_id"`
	Name      string `json:"name"`
	// SelectedEnvironmentID is the environment the project uses; 0 is none.
	SelectedEnvironmentID int64 `json:"selected_environment_id"`
}
