package models

import "time"

// FavouriteCollection is a user-defined group of favourited HTTP requests.
// It belongs to a profile and references requests without owning them.
type FavouriteCollection struct {
	ID        int64     `json:"id"`
	ProfileID int64     `json:"profile_id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}
