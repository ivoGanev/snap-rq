package models

// EnvironmentVariable is a single key/value pair belonging to an Environment.
//
// A Secret variable's value is stored encrypted (backend/secrets) and is never
// sent to the frontend: lists return it with an empty Value and HasValue set.
// It is decrypted only to send a request or when the user reveals it.
type EnvironmentVariable struct {
	ID            int64  `json:"id"`
	EnvironmentID int64  `json:"environment_id"`
	Key           string `json:"key"`
	Value         string `json:"value"`
	Secret        bool   `json:"secret"`
	HasValue      bool   `json:"has_value"`
}
