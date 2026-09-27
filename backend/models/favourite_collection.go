package models

import "time"

// FavouriteCollection is a user-defined group of favourited HTTP requests.
// It belongs to a project and references requests without owning them.
type FavouriteCollection struct {
	ID         int64               `json:"id"`
	ProjectID  int64               `json:"project_id"`
	Name       string              `json:"name"`
	CreatedAt  time.Time           `json:"created_at"`
	Appearance FavouriteAppearance `json:"appearance"`
}
