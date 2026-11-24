package models

// Collection represents a group of HTTP requests within a project.
type Collection struct {
	ID         int64                `json:"id"`
	ProjectID  int64                `json:"project_id"`
	Name       string               `json:"name"`
	Appearance CollectionAppearance `json:"appearance"`
	// LastUsedAt is the latest send of any of its requests (Unix ns, 0 =
	// never). Only collection.list fills it in.
	LastUsedAt int64 `json:"last_used_at"`
}
