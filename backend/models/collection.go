package models

// Collection represents a group of HTTP requests within a project.
type Collection struct {
	ID        int64                  `json:"id"`
	ProjectID int64                  `json:"project_id"`
	Name      string                 `json:"name"`
	Appearance CollectionAppearance `json:"appearance"`
}
