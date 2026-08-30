package models

// EnvironmentVariable is a single key/value pair belonging to an Environment.
type EnvironmentVariable struct {
	ID            int64  `json:"id"`
	EnvironmentID int64  `json:"environment_id"`
	Key           string `json:"key"`
	Value         string `json:"value"`
}
