package models

// IconReplacement maps an old (superseded) icon id to its replacement.
// Used by the frontend to drive the one-time appearance migration.
type IconReplacement struct {
	OldID string `json:"old_id"`
	NewID string `json:"new_id"`
}
